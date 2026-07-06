package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var pushAppName string
var pushNonInteractiveFlag bool
var pushVerboseFlag bool

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Deploy the current directory to fyra.sh",
	RunE:  runPush,
}

func init() {
	pushCmd.Flags().StringVar(&pushAppName, "appname", "", "app slug to push to (overrides .deploy.yaml)")
	pushCmd.Flags().BoolVar(&pushNonInteractiveFlag, "non-interactive", false, "skip the live progress TUI (auto-enabled when stdout is not a TTY)")
	pushCmd.Flags().BoolVarP(&pushVerboseFlag, "verbose", "v", false, "list uploaded, deleted, and ignored files after the push")
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

	if absP, err := absCwd(); err == nil && slug != "" {
		_ = appindex.Register(slug, absP)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	// Surface any secret files the push withholds by default, so the user knows
	// sensitive files that were not gitignored are still being kept out of the
	// deploy. Written to stderr (not stdout) so the notice stays out of any
	// piped `fyra push` output a CI/CD job might consume, and printed before the
	// TUI starts so it stays visible on both the interactive and non-interactive
	// paths.
	ignorer, _ := loadIgnoreFile(".")
	warnSkippedSecrets(os.Stderr, skippedSecretFiles(".", ignorer))

	// Non-interactive path: explicit flag OR auto-detected when stdout is not
	// a TTY (e.g. CI, piped to a file, or any wrapping that strips the TTY).
	if pushNonInteractiveFlag || !term.IsTerminal(int(os.Stdout.Fd())) {
		return runPushNonInteractive(cmd.Context(), cfg, slug, appDomain, deployConfig, os.Stdout)
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

	// Print the completed upload summary. A nil diffResult means hashing
	// failed and we degraded to a full push; treat that as pushKindFull.
	kind := pushKindFull
	if pm.diffResult != nil {
		kind = pm.diffResult.kind
	}
	renderPushSummary(os.Stdout, kind, pm.diffResult, pm.totalBytes, pm.fileCount, pm.skippedCount, pm.url, pm.firstDeploy, pm.savedAppFile)
	if pushVerboseFlag {
		renderVerboseListing(os.Stdout, pm.diffResult, pm.ignored)
	}
	return nil
}

// scanDir counts files and total uncompressed bytes in dir (excluding .git, node_modules, .deploy.yaml).
func scanDir(dir string, ign *gitignore.GitIgnore) (fileCount int, skippedCount int, totalBytes int64, ignored []string, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if shouldSkip(d.Name(), rel, d.IsDir(), ign) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			skippedCount++
			ignored = append(ignored, rel)
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

	return fileCount, skippedCount, totalBytes, ignored, err
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

// renderPushSummary prints the post-push summary to out. Called by both the
// interactive path (after the TUI exits) and the non-interactive path (after
// the stream completes). kind picks the summary line; diff carries the
// per-kind counts. diff may be nil only when kind == pushKindFull (hashing
// failed upstream and we degraded to a full push).
func renderPushSummary(out io.Writer, kind pushKind, diff *diffResult, totalBytes int64, fileCount, skippedCount int, url string, firstDeploy, savedAppFile bool) {
	switch kind {
	case pushKindNone:
		n := 0
		if diff != nil {
			n = diff.unchanged
		}
		fmt.Fprintln(out, tui.StyleSuccess.Render(fmt.Sprintf("Already up to date — %d file(s) unchanged.", n)))
	case pushKindIncremental:
		fmt.Fprintf(out, "Uploaded %s (%d changed, %d deleted, %d unchanged)\n",
			formatBytes(totalBytes), fileCount, len(diff.toDelete), diff.unchanged)
	case pushKindFull:
		fmt.Fprintf(out, "Uploaded %s (%d files)\n", formatBytes(totalBytes), fileCount)
	}

	fmt.Fprintf(out, "%d file(s) ignored\n", skippedCount)

	if kind == pushKindNone {
		return
	}

	if firstDeploy {
		fmt.Fprintf(out, "Live: https://%s\n", url)
		fmt.Fprintln(out, tui.StyleMuted.Render("First deploy — DNS may take a moment to propagate."))
	} else {
		fmt.Fprintf(out, "Done: https://%s\n", url)
	}
	if savedAppFile {
		fmt.Fprintf(out, tui.StyleMuted.Render("Saved .deploy.yaml — run '%s push' next time.\n"), binaryName)
	}
}

// renderVerboseListing prints categorized lists of uploaded, deleted, and
// ignored files. Called only when --verbose is set. Empty sections are
// omitted. All lists are sorted alphabetically for deterministic output.
func renderVerboseListing(out io.Writer, diff *diffResult, ignored []string) {
	var uploaded []string
	if diff != nil {
		switch diff.kind {
		case pushKindIncremental:
			uploaded = make([]string, 0, len(diff.toUpload))
			for p := range diff.toUpload {
				uploaded = append(uploaded, p)
			}
		case pushKindFull:
			uploaded = make([]string, 0, len(diff.localFiles))
			for p := range diff.localFiles {
				uploaded = append(uploaded, p)
			}
		}
	}

	var deleted []string
	if diff != nil {
		deleted = append(deleted, diff.toDelete...)
	}

	hasSection := false
	if len(uploaded) > 0 {
		slices.Sort(uploaded)
		if !hasSection {
			fmt.Fprintln(out)
			hasSection = true
		}
		fmt.Fprintln(out, tui.StyleMuted.Render("Uploaded:"))
		for _, p := range uploaded {
			fmt.Fprintf(out, "  %s\n", p)
		}
	}
	if len(deleted) > 0 {
		slices.Sort(deleted)
		if !hasSection {
			fmt.Fprintln(out)
			hasSection = true
		}
		fmt.Fprintln(out, tui.StyleMuted.Render("Deleted:"))
		for _, p := range deleted {
			fmt.Fprintf(out, "  %s\n", p)
		}
	}
	if len(ignored) > 0 {
		sortedIgnored := append([]string(nil), ignored...)
		slices.Sort(sortedIgnored)
		if !hasSection {
			fmt.Fprintln(out)
			hasSection = true
		}
		fmt.Fprintln(out, tui.StyleMuted.Render("Ignored:"))
		for _, p := range sortedIgnored {
			fmt.Fprintf(out, "  %s\n", p)
		}
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
		if strings.Contains(err.Error(), "password protection") {
			return fmt.Errorf("this site uses .auth password protection, which isn't available on your plan. Remove the .auth file(s) and push again, or upgrade your plan to keep using it.")
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

// pruneDir reports whether a directory should be pruned from the walk entirely
// (never descended into). Centralizing the list here keeps shouldSkip and
// skippedSecretFiles from drifting: a directory added here is skipped by the
// tarball AND excluded from the secret-file warning in lockstep.
func pruneDir(name string) bool {
	return name == ".git" || name == "node_modules"
}

// shouldSkip returns true if the file or directory should be excluded from the tarball.
func shouldSkip(name, relPath string, isDir bool, ign *gitignore.GitIgnore) bool {
	if isDir && pruneDir(name) {
		return true
	}
	if name == ".deploy.yaml" {
		return true
	}
	// Hardcoded secret exclusions — these must never ship even if the
	// user forgot to list them in .fyraignore. Matched on base name so
	// rules apply at any depth (e.g. apps/api/.env.production).
	if isSecretFile(name) {
		return true
	}
	if ign != nil && ign.MatchesPath(relPath) {
		return true
	}
	return false
}

// isSecretFile reports whether name matches a hardcoded secret-file pattern
// that must never be uploaded. Matched on base name, so it applies at any
// depth (e.g. apps/api/.env.production). Used by both shouldSkip (to exclude
// the file) and skippedSecretFiles (to report it).
func isSecretFile(name string) bool {
	return strings.HasPrefix(name, ".env") ||
		strings.HasSuffix(name, ".pem") ||
		strings.HasSuffix(name, ".key") ||
		strings.HasSuffix(name, ".p12") ||
		strings.HasSuffix(name, ".pfx") ||
		name == ".netrc"
}

// skippedSecretFiles walks dir and returns the relative paths of files that the
// hardcoded secret rule withholds from the push AND that the user has not
// already excluded via .fyraignore. A secret the user already ignores is
// intentional and needs no warning; this surfaces only the ones that would
// otherwise have shipped. Directories pruned from the push (.git, node_modules)
// are not descended into.
func skippedSecretFiles(dir string, ign *gitignore.GitIgnore) []string {
	var found []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: never block a push on a walk error
		}
		if d.IsDir() {
			if pruneDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSecretFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		if ign != nil && ign.MatchesPath(rel) {
			return nil
		}
		found = append(found, rel)
		return nil
	})
	sort.Strings(found)
	return found
}

// warnSkippedSecrets prints a one-time notice listing secret files withheld
// from the push. It is a no-op when files is empty.
func warnSkippedSecrets(out io.Writer, files []string) {
	if len(files) == 0 {
		return
	}
	noun := "file"
	if len(files) > 1 {
		noun = "files"
	}
	fmt.Fprintf(out, "Skipped %d secret %s (excluded by default, not uploaded):\n", len(files), noun)
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", f)
	}
}

// sha256File returns the SHA256 hex of a file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// computeDiff compares local file hashes against the server manifest.
// Returns paths to upload (new + modified) and paths to delete (on server but not local).
func computeDiff(localFiles, serverManifest map[string]string) (toUpload []string, toDelete []string) {
	for path, localHash := range localFiles {
		serverHash, exists := serverManifest[path]
		if !exists || serverHash != localHash {
			toUpload = append(toUpload, path)
		}
	}
	for path := range serverManifest {
		if _, exists := localFiles[path]; !exists {
			toDelete = append(toDelete, path)
		}
	}
	return toUpload, toDelete
}

// fetchManifest contacts the server and returns the current deploy manifest.
// Returns nil manifest (no error) if the app has never been deployed.
func fetchManifest(ctx context.Context, cfg clientConfig, slug, domain string) (map[string]string, error) {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	resp, err := client.GetDeployManifest(authContext(ctx, cfg.Token), &pb.GetDeployManifestRequest{
		SlugName: slug,
		Domain:   domain,
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			return nil, nil // first deploy, no manifest
		}
		return nil, fmt.Errorf("get manifest: %w", err)
	}
	return resp.Files, nil
}

// scanDirWithHashes walks the directory and returns a map of relative path →
// SHA256 hash and the number of files skipped. Uses the same exclusion rules as scanDir.
func scanDirWithHashes(dir string, ign *gitignore.GitIgnore) (files map[string]string, skippedCount int, ignored []string, err error) {

	files = make(map[string]string)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if shouldSkip(d.Name(), rel, d.IsDir(), ign) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			skippedCount++
			ignored = append(ignored, rel)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		hash, err := sha256File(path)
		if err != nil {
			return err
		}
		files[rel] = hash
		return nil
	})
	return files, skippedCount, ignored, err
}

// tarballDiffDir writes a gzipped tar of only the files in uploadSet to w.
// uploadSet keys are relative paths (e.g. "sub/file.txt").
func tarballDiffDir(dir string, w io.Writer, uploadSet map[string]bool) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for relPath := range uploadSet {
		path := filepath.Join(dir, relPath)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = relPath
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}

	tw.Close() //nolint:errcheck
	gw.Close() //nolint:errcheck
	return nil
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

// pushStreamFn opens a streaming Push RPC. Production passes a closure over
// client.Push; tests pass a stub that returns a fake stream.
type pushStreamFn func(ctx context.Context) (pb.DeployService_PushClient, error)

// manifestFn fetches the server's deploy manifest for diffing. Production
// passes a closure over fetchManifest; tests pass a stub.
type manifestFn func(ctx context.Context, slug, domain string) (map[string]string, error)

// runPushNonInteractive is the CI-friendly entry point. It dials once, builds
// the stream and manifest seam closures, then delegates to the testable core.
func runPushNonInteractive(ctx context.Context, cfg clientConfig, slug, domain string, deployConfig map[string]interface{}, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()

	openStream := func(ctx context.Context) (pb.DeployService_PushClient, error) {
		return client.Push(ctx)
	}
	manifestFetch := func(ctx context.Context, slug, domain string) (map[string]string, error) {
		return fetchManifest(ctx, cfg, slug, domain)
	}
	return pushNonInteractive(ctx, cfg, slug, domain, pushAppName != "", deployConfig, out, openStream, manifestFetch)
}

// pushNonInteractive is the testable core of the non-interactive push path.
// The seam parameters (openStream, fetchManifestFn) keep this function off the
// network so unit tests can exercise diff logic, error mapping, and output
// formatting with stubs.
func pushNonInteractive(
	ctx context.Context,
	cfg clientConfig,
	slug, domain string,
	saveAppFile bool,
	deployConfig map[string]interface{},
	out io.Writer,
	openStream pushStreamFn,
	fetchManifestFn manifestFn,
) error {

	var (
		diff         *diffResult
		totalBytes   int64
		fileCount    int
		skippedCount int
		ignored      []string
	)

	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	ignorer, _ := loadIgnoreFile(".")

	// Local file hashes feed both the diff and the manifest we send on first chunk.
	localFiles, skippedCount, ignored, err := scanDirWithHashes(".", ignorer)
	if err != nil {
		return fmt.Errorf("hash files: %w", err)
	}

	// Fetch server manifest. Errors here are non-fatal — fall back to full push.
	serverManifest, _ := fetchManifestFn(ctx, slug, domain)

	switch {
	case len(serverManifest) > 0:
		uploadPaths, toDelete := computeDiff(localFiles, serverManifest)
		if len(uploadPaths) == 0 && len(toDelete) == 0 {
			diff = &diffResult{
				kind:       pushKindNone,
				localFiles: localFiles,
				unchanged:  len(localFiles),
			}
			renderPushSummary(out, pushKindNone, diff, 0, 0, skippedCount, "", false, false)
			if pushVerboseFlag {
				renderVerboseListing(out, diff, ignored)
			}
			return nil
		}

		uploadSet := make(map[string]bool, len(uploadPaths))
		var uploadBytes int64
		for _, p := range uploadPaths {
			uploadSet[p] = true
			if info, err := os.Stat(p); err == nil {
				uploadBytes += info.Size()
			}
		}
		diff = &diffResult{
			kind:        pushKindIncremental,
			toUpload:    uploadSet,
			toDelete:    toDelete,
			localFiles:  localFiles,
			uploadCount: len(uploadPaths),
			uploadBytes: uploadBytes,
			unchanged:   len(localFiles) - len(uploadPaths),
		}
		totalBytes = uploadBytes
		fileCount = len(uploadPaths)

	default:
		// First deploy or fetch failure: full push, still send the manifest so
		// the server can save it for the next diff.
		_, skippedCount, totalBytes, ignored, err = scanDir(".", ignorer)
		if err != nil {
			return fmt.Errorf("scan directory: %w", err)
		}
		diff = &diffResult{kind: pushKindFull, localFiles: localFiles}
		fileCount = len(localFiles)
	}

	authCtx := authContext(ctx, cfg.Token)
	stream, err := openStream(authCtx)
	if err != nil {
		return fmt.Errorf("open push stream: %w", err)
	}

	// Drain progress messages — we don't render a live bar in non-interactive
	// mode, but stream{Dir,Diff}WithProgress still emits on the channel and
	// would block once the 64-message buffer fills.
	progressCh := make(chan pushProgressMsg, 64)
	progressDone := make(chan struct{})
	go func() {
		for range progressCh {
		}
		close(progressDone)
	}()

	if diff.kind == pushKindFull {
		err = streamDirWithProgress(".", slug, domain, deployConfig, stream, totalBytes, progressCh, ignorer, diff.localFiles)
	} else {
		err = streamDiffWithProgress(".", slug, domain, deployConfig, stream, totalBytes, progressCh, diff)
	}
	close(progressCh)
	<-progressDone
	if err != nil {
		return err
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return friendlyPushError(err)
	}

	savedAppFile := false
	if saveAppFile {
		if err := writeAppFile(appFile{Slug: slug, Server: cfg.ServerAddress}); err != nil {
			return fmt.Errorf("save .deploy.yaml: %w", err)
		}
		savedAppFile = true
	}

	renderPushSummary(out, diff.kind, diff, totalBytes, fileCount, skippedCount, resp.Url, resp.FirstDeploy, savedAppFile)
	if pushVerboseFlag {
		renderVerboseListing(out, diff, ignored)
	}
	return nil
}
