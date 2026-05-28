package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// pushStep represents the current step in the push flow.
type pushStep int

const (
	stepPushScanning pushStep = iota
	stepPushDiffing
	stepPushUploading
	stepPushDeploying
	stepPushDone
)

// pushModel is the Bubble Tea model for the push command.
type pushModel struct {
	step         pushStep
	slug         string
	domain       string
	cfg          clientConfig
	ctx          context.Context
	spinner      spinner.Model
	bar          progress.Model
	fileCount    int
	totalBytes   int64
	sent         int64
	url          string
	firstDeploy  bool
	savedAppFile bool
	err          error
	planErr      error
	progressCh   chan pushProgressMsg
	ignorer      *gitignore.GitIgnore
	deployConfig map[string]interface{}
	diffResult   *diffResult // nil if full push
}

// pushProgressMsg carries upload progress.
type pushProgressMsg struct {
	sent  int64
	total int64
}

// pushResultMsg carries the final push response or error.
type pushResultMsg struct {
	url         string
	firstDeploy bool
	err         error
}

// scanDoneMsg signals that directory scanning is complete.
type scanDoneMsg struct {
	fileCount  int
	totalBytes int64
}

// diffResult holds the result of diff computation.
type diffResult struct {
	toUpload    map[string]bool   // paths to include in tarball
	toDelete    []string          // paths to delete from server
	localFiles  map[string]string // full local manifest (to send to server)
	uploadCount int
	uploadBytes int64
	unchanged   int
	fullPush    bool // true when falling back to full push (first deploy or fetch error)
}

// diffDoneMsg signals that diff computation is complete.
type diffDoneMsg struct {
	result *diffResult
	err    error
}

func newPushModel(slug, domain string, cfg clientConfig, ctx context.Context, deployConfig map[string]interface{}) pushModel {
	bar := progress.New(progress.WithGradient(string(tui.ColorPrimary), string(tui.ColorSuccess)))
	ignorer, _ := loadIgnoreFile(".")

	return pushModel{
		step:         stepPushScanning,
		slug:         slug,
		domain:       domain,
		cfg:          cfg,
		ctx:          ctx,
		spinner:      tui.NewSpinner(),
		bar:          bar,
		progressCh:   make(chan pushProgressMsg, 64),
		ignorer:      ignorer,
		deployConfig: deployConfig,
	}
}

func (m pushModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return pushResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.doScan)
}

//nolint:revive // cyclomatic is inherent to a state-machine Update.
func (m pushModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}

	case scanDoneMsg:
		return m.handleScanDone(msg)

	case diffDoneMsg:
		return m.handleDiffDone(msg)

	case pushProgressMsg:
		m.sent = msg.sent
		m.totalBytes = msg.total
		ratio := 0.0
		if msg.total > 0 {
			ratio = float64(msg.sent) / float64(msg.total)
		}
		barCmd := m.bar.SetPercent(ratio)
		return m, tea.Batch(barCmd, waitForProgress(m.progressCh))

	case pushResultMsg:
		return m.handleResult(msg)

	}

	// Delegate to the active sub-model.
	var cmd tea.Cmd
	switch m.step {
	case stepPushScanning, stepPushDeploying:
		m.spinner, cmd = m.spinner.Update(msg)
	case stepPushUploading:
		model, updateCmd := m.bar.Update(msg)
		if pm, ok := model.(progress.Model); ok {
			m.bar = pm
		}
		cmd = updateCmd
	}
	return m, cmd
}

func (m pushModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepPushScanning:
		b.WriteString(m.spinner.View() + " Scanning " + m.slug + "...")

	case stepPushDiffing:
		b.WriteString(m.spinner.View() + " Checking for changes...")

	case stepPushUploading:
		pct := float64(0)
		if m.totalBytes > 0 {
			pct = float64(m.sent) * 100 / float64(m.totalBytes)
		}
		b.WriteString(fmt.Sprintf("Uploading %s (%d files)\n", formatBytes(m.totalBytes), m.fileCount))
		b.WriteString(m.bar.View())
		b.WriteString(fmt.Sprintf(" %.0f%% — %s / %s", pct, formatBytes(m.sent), formatBytes(m.totalBytes)))

	case stepPushDeploying:
		b.WriteString(m.spinner.View() + " Deploying on server...")

	case stepPushDone:
		if m.planErr != nil {
			b.WriteString(tui.PlanLimitBlock(m.planErr.Error()))
		} else if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		}
	}

	return b.String()
}

// doScan is a tea.Cmd that counts files in the directory.
func (m pushModel) doScan() tea.Msg {
	fileCount, totalBytes, err := scanDir(".", m.ignorer)
	if err != nil {
		return pushResultMsg{err: fmt.Errorf("scan directory: %w", err)}
	}
	return scanDoneMsg{fileCount: fileCount, totalBytes: totalBytes}
}

// handleScanDone transitions from scanning to diffing.
func (m pushModel) handleScanDone(msg scanDoneMsg) (tea.Model, tea.Cmd) {
	m.fileCount = msg.fileCount
	m.totalBytes = msg.totalBytes
	m.step = stepPushDiffing
	return m, m.doDiff
}

// handleDiffDone handles the result of diff computation.
func (m pushModel) handleDiffDone(msg diffDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.step = stepPushUploading
		return m, tea.Batch(m.startUpload(), waitForProgress(m.progressCh))
	}

	if msg.result != nil && !msg.result.fullPush && msg.result.toUpload == nil && msg.result.toDelete == nil {
		// Already up to date — nothing changed.
		m.diffResult = msg.result
		m.step = stepPushDone
		return m, tea.Quit
	}

	m.diffResult = msg.result
	m.step = stepPushUploading

	if msg.result != nil && !msg.result.fullPush {
		m.fileCount = msg.result.uploadCount
		m.totalBytes = msg.result.uploadBytes
	}

	return m, tea.Batch(m.startUpload(), waitForProgress(m.progressCh))
}

// doDiff fetches the server manifest and computes the diff.
func (m pushModel) doDiff() tea.Msg {
	// Compute local file hashes.
	localFiles, err := scanDirWithHashes(".", m.ignorer)
	if err != nil {
		return diffDoneMsg{err: fmt.Errorf("hash files: %w", err)}
	}

	// Fetch server manifest.
	serverManifest, err := fetchManifest(m.ctx, m.cfg, m.slug, m.domain)
	if err != nil {
		// Non-fatal: fall back to full push, but still send manifest.
		return diffDoneMsg{result: &diffResult{localFiles: localFiles, fullPush: true}}
	}

	if serverManifest == nil || len(serverManifest) == 0 {
		// First deploy — full push, but send manifest so server saves it.
		return diffDoneMsg{result: &diffResult{localFiles: localFiles, fullPush: true}}
	}

	// Compute diff.
	uploadPaths, toDelete := computeDiff(localFiles, serverManifest)

	if len(uploadPaths) == 0 && len(toDelete) == 0 {
		// Already up to date.
		return diffDoneMsg{result: &diffResult{
			toUpload:   nil,
			toDelete:   nil,
			localFiles: localFiles,
			unchanged:  len(localFiles),
		}}
	}

	// Calculate upload size for progress bar.
	uploadSet := make(map[string]bool, len(uploadPaths))
	var uploadBytes int64
	for _, p := range uploadPaths {
		uploadSet[p] = true
		if info, err := os.Stat(p); err == nil {
			uploadBytes += info.Size()
		}
	}

	return diffDoneMsg{result: &diffResult{
		toUpload:    uploadSet,
		toDelete:    toDelete,
		localFiles:  localFiles,
		uploadCount: len(uploadPaths),
		uploadBytes: uploadBytes,
		unchanged:   len(localFiles) - len(uploadPaths),
	}}
}

// startUpload is a tea.Cmd that performs the GRPC streaming upload.
func (m pushModel) startUpload() tea.Cmd {
	return func() tea.Msg {
		client, cleanup, err := m.cfg.dial()
		if err != nil {
			return pushResultMsg{err: err}
		}
		defer cleanup()

		ctx := authContext(m.ctx, m.cfg.Token)
		stream, err := client.Push(ctx)
		if err != nil {
			return pushResultMsg{err: fmt.Errorf("open push stream: %w", err)}
		}

		if m.diffResult != nil && m.diffResult.toUpload != nil {
			// Diff push — send only changed files.
			if err := streamDiffWithProgress(".", m.slug, m.domain, m.deployConfig, stream, m.totalBytes, m.progressCh, m.diffResult); err != nil {
				return pushResultMsg{err: err}
			}
		} else {
			// Full push — send manifest so server can save it for next diff.
			var manifest map[string]string
			if m.diffResult != nil {
				manifest = m.diffResult.localFiles
			}
			if err := streamDirWithProgress(".", m.slug, m.domain, m.deployConfig, stream, m.totalBytes, m.progressCh, m.ignorer, manifest); err != nil {
				return pushResultMsg{err: err}
			}
		}

		resp, err := stream.CloseAndRecv()
		if err != nil {
			return pushResultMsg{err: friendlyPushError(err)}
		}

		return pushResultMsg{url: resp.Url, firstDeploy: resp.FirstDeploy}
	}
}

// handleResult transitions to the done state.
func (m pushModel) handleResult(msg pushResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if isEmailNotConfirmed(msg.err) {
			m.err = fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
			m.step = stepPushDone
			return m, tea.Quit
		}
		code := status.Code(msg.err)
		if code == codes.ResourceExhausted || code == codes.PermissionDenied {
			m.planErr = msg.err
		} else {
			m.err = msg.err
		}
		m.step = stepPushDone
		return m, tea.Quit
	}

	m.url = msg.url
	m.firstDeploy = msg.firstDeploy

	if pushAppName != "" {
		if err := writeAppFile(appFile{Slug: m.slug, Server: m.cfg.ServerAddress}); err != nil {
			m.err = err
			m.step = stepPushDone
			return m, tea.Quit
		}
		m.savedAppFile = true
	}

	m.step = stepPushDone
	return m, tea.Quit
}

// waitForProgress blocks until a progress message arrives on the channel.
func waitForProgress(ch chan pushProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// streamDirWithProgress streams a tarball of dir to stream, reporting progress via ch.
func streamDirWithProgress(dir, slug, domain string, deployConfig map[string]interface{}, stream pb.DeployService_PushClient, total int64, ch chan pushProgressMsg, ign *gitignore.GitIgnore, manifest map[string]string) error {
	pr, pw := io.Pipe()

	go func() {
		pw.CloseWithError(tarballDir(dir, pw, ign))
	}()

	var configProto *structpb.Struct
	if len(deployConfig) > 0 {
		var err error
		configProto, err = structpb.NewStruct(deployConfig)
		if err != nil {
			return fmt.Errorf("marshal deploy config: %w", err)
		}
	}

	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	first := true
	var sent int64

	for {
		n, err := pr.Read(buf)
		if n > 0 {
			req := &pb.PushRequest{Chunk: buf[:n]}
			if first {
				req.SlugName = slug
				req.Domain = domain
				req.Config = configProto
				req.Manifest = manifest
				first = false
			}
			if sendErr := stream.Send(req); sendErr != nil {
				pr.CloseWithError(sendErr)
				if sendErr == io.EOF {
					return nil
				}
				return fmt.Errorf("send chunk: %w", sendErr)
			}
			sent += int64(n)
			ch <- pushProgressMsg{sent: sent, total: total}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tarball: %w", err)
		}
	}
		return nil
	}

// streamDiffWithProgress streams a diff tarball to the server with progress reporting.
func streamDiffWithProgress(dir, slug, domain string, deployConfig map[string]interface{}, stream pb.DeployService_PushClient, total int64, ch chan pushProgressMsg, diff *diffResult) error {
	pr, pw := io.Pipe()

	go func() {
		pw.CloseWithError(tarballDiffDir(dir, pw, diff.toUpload))
	}()

	var configProto *structpb.Struct
	if len(deployConfig) > 0 {
		var err error
		configProto, err = structpb.NewStruct(deployConfig)
		if err != nil {
			return fmt.Errorf("marshal deploy config: %w", err)
		}
	}

	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	first := true
	var sent int64

	for {
		n, err := pr.Read(buf)
		if n > 0 {
			req := &pb.PushRequest{Chunk: buf[:n]}
			if first {
				req.SlugName = slug
				req.Domain = domain
				req.Config = configProto
				req.Deletes = diff.toDelete
				req.Manifest = diff.localFiles
				first = false
			}
			if sendErr := stream.Send(req); sendErr != nil {
				pr.CloseWithError(sendErr)
				if sendErr == io.EOF {
					return nil
				}
				return fmt.Errorf("send chunk: %w", sendErr)
			}
			sent += int64(n)
			ch <- pushProgressMsg{sent: sent, total: total}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tarball: %w", err)
		}
	}
	return nil
}