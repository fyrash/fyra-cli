package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pushStep represents the current step in the push flow.
type pushStep int

const (
	stepPushScanning pushStep = iota
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

func newPushModel(slug, domain string, cfg clientConfig, ctx context.Context) pushModel {
	bar := progress.New(progress.WithGradient(string(tui.ColorPrimary), string(tui.ColorSuccess)))
	ignorer, _ := loadIgnoreFile(".")

	return pushModel{
		step:       stepPushScanning,
		slug:       slug,
		domain:     domain,
		cfg:        cfg,
		ctx:        ctx,
		spinner:    tui.NewSpinner(),
		bar:        bar,
		progressCh: make(chan pushProgressMsg, 64),
		ignorer:    ignorer,
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

// handleScanDone transitions from scanning to uploading.
func (m pushModel) handleScanDone(msg scanDoneMsg) (tea.Model, tea.Cmd) {
	m.fileCount = msg.fileCount
	m.totalBytes = msg.totalBytes
	m.step = stepPushUploading
	return m, tea.Batch(m.startUpload(), waitForProgress(m.progressCh))
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

		if err := streamDirWithProgress(".", m.slug, m.domain, stream, m.totalBytes, m.progressCh, m.ignorer); err != nil {
			return pushResultMsg{err: err}
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
func streamDirWithProgress(dir, slug, domain string, stream pb.DeployService_PushClient, total int64, ch chan pushProgressMsg, ign *gitignore.GitIgnore) error {
	pr, pw := io.Pipe()

	go func() {
		pw.CloseWithError(tarballDir(dir, pw, ign))
	}()

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
