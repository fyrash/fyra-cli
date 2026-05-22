package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var pushAppName string

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Deploy the current directory to fyra.sh",
	RunE:  runPush,
}

func init() {
	pushCmd.Flags().StringVar(&pushAppName, "appname", "", "app slug to push to (overrides .deploy.yaml)")
}

func runPush(cmd *cobra.Command, _ []string) error {
	var slug, appDomain string
	var deployConfig map[string]interface{}
	if pushAppName != "" {
		slug = pushAppName
	} else {
		af, err := readAppFile()
		if err != nil {
			return err
		}
		slug = af.Slug
		appDomain = af.Domain
		deployConfig = af.Config
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	m := newPushModel(slug, appDomain, cfg, cmd.Context(), deployConfig)
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	pm, ok := final.(pushModel)
	if !ok || pm.err != nil {
		if ok {
			return pm.err
		}
		return fmt.Errorf("unexpected error")
	}

	// Print the completed upload summary with a static progress bar.
	bar := progress.New(progress.WithGradient(string(tui.ColorPrimary), string(tui.ColorSuccess)), progress.WithoutPercentage())
	fmt.Printf("Uploading %s (%d files)\n", formatBytes(pm.totalBytes), pm.fileCount)
	fmt.Printf("%s 100%% — %s / %s\n", bar.ViewAs(1.0), formatBytes(pm.totalBytes), formatBytes(pm.totalBytes))

	if pm.firstDeploy {
		fmt.Printf("Live: https://%s\n", pm.url)
		fmt.Println(tui.StyleMuted.Render("First deploy — DNS may take a moment to propagate."))
	} else {
		fmt.Printf("Done: https://%s\n", pm.url)
	}
	if pm.savedAppFile {
		fmt.Printf(tui.StyleMuted.Render("Saved .deploy.yaml — run '%s push' next time.\n"), binaryName)
	}
	return nil
}

// scanDir counts files and total uncompressed bytes in dir (excluding .git, node_modules, .deploy.yaml).
func scanDir(dir string, ign *gitignore.GitIgnore) (fileCount int, totalBytes int64, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if shouldSkip(d.Name(), rel, d.IsDir(), ign) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fileCount++
		totalBytes += info.Size()
		return nil
	})
	return
}

// formatBytes returns a human-readable byte size (KB/MB/GB).
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func friendlyPushError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("app not found — check the name with '%s list'", binaryName)
	case codes.PermissionDenied:
		if strings.Contains(err.Error(), "email not confirmed") {
			return fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
		}
		return fmt.Errorf("you don't have permission to push to that app")
	case codes.Unauthenticated:
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	case codes.Unavailable:
		return fmt.Errorf("no deployment server available — try again later")
	default:
		return fmt.Errorf("push failed: %w", err)
	}
}

// loadIgnoreFile reads .fyraignore from dir. Returns nil if the file does not exist.
func loadIgnoreFile(dir string) (*gitignore.GitIgnore, error) {
	path := filepath.Join(dir, ".fyraignore")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	return gitignore.CompileIgnoreFile(path)
}

// shouldSkip returns true if the file or directory should be excluded from the tarball.
func shouldSkip(name, relPath string, isDir bool, ign *gitignore.GitIgnore) bool {
	if isDir && (name == ".git" || name == "node_modules") {
		return true
	}
	if name == ".deploy.yaml" {
		return true
	}
	if ign != nil && ign.MatchesPath(relPath) {
		return true
	}
	return false
}

// tarballDir writes a gzipped tar of dir to w, skipping .git, node_modules, .deploy.yaml,
// and any patterns matched by .fyraignore.
func tarballDir(dir string, w io.Writer, ign *gitignore.GitIgnore) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if shouldSkip(d.Name(), rel, d.IsDir(), ign) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})

	tw.Close() //nolint:errcheck
	gw.Close() //nolint:errcheck
	return err
}
