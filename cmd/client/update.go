// cmd/client/update.go
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

const (
	versionURL     = "http://downloads.fyra.sh/client/latest/version.txt"
	updateCheckTTL = 24 * time.Hour
	httpTimeout    = 5 * time.Second
)

// parseGitDescribe extracts the base tag and commit distance from a git-describe
// version string of the form vX.Y.Z-N-gHASH. Returns ("", -1) if the string
// does not match that pattern.
func parseGitDescribe(v string) (base string, commits int, ok bool) {
	// Expected suffix: -N-gHASH
	// Walk backwards: last segment must start with 'g' (the hash), second-to-last
	// must be a decimal integer.
	parts := strings.Split(v, "-")
	if len(parts) < 3 {
		return "", -1, false
	}
	hash := parts[len(parts)-1]
	if len(hash) < 2 || hash[0] != 'g' {
		return "", -1, false
	}
	n, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil || n < 0 {
		return "", -1, false
	}
	base = strings.Join(parts[:len(parts)-2], "-")
	if !semver.IsValid(semver.Canonical(base)) {
		return "", -1, false
	}
	return base, n, true
}

// isNewer returns true if latest is a higher version than current.
// Handles git-describe format (vX.Y.Z-N-gHASH) by comparing commit distance
// numerically when both versions share the same base tag.
// Returns false if either string is not a recognisable version (e.g. "dev").
func isNewer(current, latest string) bool {
	cBase, cCommits, cIsDesc := parseGitDescribe(current)
	lBase, lCommits, lIsDesc := parseGitDescribe(latest)

	if cIsDesc && lIsDesc {
		// Compare the base tags first.
		switch semver.Compare(semver.Canonical(cBase), semver.Canonical(lBase)) {
		case 1:
			return false // current base is ahead
		case -1:
			return true // latest base is ahead
		}
		// Same base tag — the one with more commits since the tag is newer.
		return lCommits > cCommits
	}

	// Fall back to plain semver comparison (e.g. clean release tags).
	c := semver.Canonical(current)
	l := semver.Canonical(latest)
	if !semver.IsValid(c) || !semver.IsValid(l) {
		return false
	}
	return semver.Compare(l, c) > 0
}

// isUpdateCheckDue returns true if more than 24 hours have passed since lastCheck.
// A zero lastCheck (never checked) is always due.
func isUpdateCheckDue(lastCheck time.Time) bool {
	if lastCheck.IsZero() {
		return true
	}
	return time.Since(lastCheck) > updateCheckTTL
}

// fetchLatestVersion GETs url and returns the trimmed version string.
func fetchLatestVersion(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// updateCheckFile returns the path to the update-check timestamp file (~/.fyra/update-check).
func updateCheckFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".fyra", "update-check"), nil
}

// readLastCheckTime reads the Unix timestamp from the update-check file.
// Returns zero time on any error (file missing = never checked).
func readLastCheckTime() time.Time {
	path, err := updateCheckFile()
	if err != nil {
		return time.Time{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// writeLastCheckTime writes the current time as a Unix timestamp to the update-check file.
func writeLastCheckTime() {
	path, err := updateCheckFile()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
}

// checkForUpdate is called in a background goroutine after each successful command.
// Checks at most once per 24 hours; prints a notice to stderr if newer version available.
// All errors are silently swallowed.
func checkForUpdate() {
	if !isUpdateCheckDue(readLastCheckTime()) {
		return
	}
	writeLastCheckTime()
	latest, err := fetchLatestVersion(versionURL)
	if err != nil {
		return
	}
	if isNewer(version, latest) {
		fmt.Fprintf(os.Stderr, "\nA new version is available (%s). Run '%s update' to upgrade.\n", latest, binaryName)
	}
}

// tarballURL returns the download URL for the current OS/arch.
func tarballURL() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos == "windows" {
		return "", fmt.Errorf("automatic update is not supported on Windows; download manually from https://fyra.sh")
	}
	return fmt.Sprintf("http://downloads.fyra.sh/client/latest/fyra-%s-%s.tar.gz", goos, goarch), nil
}

// progressWriter wraps an io.Writer and prints a live download progress line to stdout.
type progressWriter struct {
	w       io.Writer
	total   int64 // -1 if unknown
	written int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.written += int64(n)
	if p.total > 0 {
		pct := p.written * 100 / p.total
		fmt.Printf("\rDownloading... %s / %s (%d%%)", formatBytes(p.written), formatBytes(p.total), pct)
	} else {
		fmt.Printf("\rDownloading... %s", formatBytes(p.written))
	}
	return n, err
}

// downloadTo streams url into a new temp file created in dir (falls back to os.TempDir() on permission error).
// Returns the path to the temp file; caller must remove it.
func downloadTo(url, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, ".fyra-update-*.tar.gz")
	if err != nil {
		if !os.IsPermission(err) {
			return "", fmt.Errorf("create temp file: %w", err)
		}
		tmp, err = os.CreateTemp("", ".fyra-update-*.tar.gz")
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
	}
	defer tmp.Close()

	pw := &progressWriter{w: tmp, total: resp.ContentLength}
	if _, err := io.Copy(pw, resp.Body); err != nil {
		fmt.Println()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write download: %w", err)
	}
	fmt.Println()
	return tmp.Name(), nil
}

// extractBinary extracts the first regular file from the .tar.gz at tarPath into dir.
// Returns the path of the extracted file.
func extractBinary(tarPath, dir string) (string, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Match the exact binary name goreleaser produces: fyra-{os}-{arch}
		if filepath.Base(hdr.Name) != fmt.Sprintf("fyra-%s-%s", runtime.GOOS, runtime.GOARCH) {
			continue
		}
		out, err := os.CreateTemp(dir, ".fyra-bin-*")
		if err != nil {
			if !os.IsPermission(err) {
				return "", fmt.Errorf("create temp binary: %w", err)
			}
			out, err = os.CreateTemp("", ".fyra-bin-*")
			if err != nil {
				return "", fmt.Errorf("create temp binary: %w", err)
			}
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			os.Remove(out.Name())
			return "", fmt.Errorf("extract binary: %w", err)
		}
		out.Close()
		if err := os.Chmod(out.Name(), 0755); err != nil {
			os.Remove(out.Name())
			return "", err
		}
		// Drain remaining tar entries so gzip can verify its checksum.
		for {
			_, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				break // best-effort drain; checksum check below will catch corruption
			}
		}
		// Verify gzip checksum — catches truncated/corrupt downloads.
		if err := gr.Close(); err != nil {
			os.Remove(out.Name())
			return "", fmt.Errorf("gzip checksum: %w", err)
		}
		return out.Name(), nil
	}
	return "", fmt.Errorf("binary fyra-%s-%s not found in tarball", runtime.GOOS, runtime.GOARCH)
}

// installBinary atomically replaces dst with src.
// Tries os.Rename first; falls back to sudo mv if permission is denied.
func installBinary(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !os.IsPermission(err) {
		return fmt.Errorf("replace binary: %w", err)
	}
	sudoCmd := exec.Command("sudo", "mv", src, dst)
	sudoCmd.Stdin = os.Stdin
	sudoCmd.Stdout = os.Stdout
	sudoCmd.Stderr = os.Stderr
	if sudoErr := sudoCmd.Run(); sudoErr != nil {
		dir := filepath.Dir(dst)
		return fmt.Errorf("cannot update: no write permission to %s\nRe-run as root or reinstall to a user-writable directory", dir)
	}
	return nil
}

func runUpdate(_ *cobra.Command, _ []string) error {
	latest, err := fetchLatestVersion(versionURL)
	if err != nil {
		return fmt.Errorf("check latest version: %w", err)
	}

	if !isNewer(version, latest) {
		fmt.Printf("Already up to date (%s).\n", version)
		return nil
	}

	fmt.Printf("Updating %s → %s...\n", version, latest)

	tarURL, err := tarballURL()
	if err != nil {
		return err
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}
	binaryDir := filepath.Dir(binaryPath)

	tarPath, err := downloadTo(tarURL, binaryDir)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(tarPath)

	binPath, err := extractBinary(tarPath, binaryDir)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	defer os.Remove(binPath)

	if err := installBinary(binPath, binaryPath); err != nil {
		return err
	}

	fmt.Printf("Updated to %s.\n", latest)
	return nil
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the fyra CLI to the latest version",
	RunE:  runUpdate,
}
