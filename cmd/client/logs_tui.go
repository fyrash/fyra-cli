package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// tickMsg is sent after a delay to trigger the next poll.
type tickMsg struct{}

// tick returns a command that sends tickMsg after the given duration.
func tick(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return tickMsg{}
	}
}

type clockTickMsg struct{}

func clockTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		return clockTickMsg{}
	}
}

type logsStep int

const (
	stepLogsLoading logsStep = iota
	stepLogsStreaming
	stepLogsInspect
)

type logsModel struct {
	step         logsStep
	spinner      spinner.Model
	vp           viewport.Model
	ready        bool // true after first tea.WindowSizeMsg
	localTime    bool
	cfg          clientConfig
	ctx          context.Context
	slug         string
	domain       string
	err          error
	retryCount   int
	paused       bool
	totalEntries int

	lastTS     string
	seen       map[string]struct{} // dedup by (ts + node_id); grows unbounded but fine for short debugging sessions
	rawEntries []*pb.RequestLogEntry
	lines      []string // formatted cache of rawEntries, rebuilt on toggle

	cursor         int
	inspectVP      viewport.Model
	inspectVPReady bool
	firstTS        string
	fetchingOlder  bool
	noOlderEntries bool

	client  pb.DeployServiceClient
	cleanup func()
}

type logsResultMsg struct {
	resp *pb.GetRequestLogsResponse
	err  error
}

type olderLogsResultMsg struct {
	resp *pb.GetRequestLogsResponse
	err  error
}

func newLogsModel(slug, domain string, cfg clientConfig, ctx context.Context) logsModel {
	client, cleanup, _ := cfg.dial() // error handled in first fetchLogs
	return logsModel{
		step:         stepLogsLoading,
		spinner:      tui.NewSpinner(),
		cfg:          cfg,
		ctx:          ctx,
		slug:         slug,
		domain:       domain,
		paused:       false,
		totalEntries: 0,
		seen:         make(map[string]struct{}),
		client:       client,
		cleanup:      cleanup,
	}
}

func (m logsModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.fetchLogs(), clockTick())
}

func (m *logsModel) close() {
	if m.cleanup != nil {
		m.cleanup()
		m.cleanup = nil
		m.client = nil
	}
}

//nolint:revive // cyclomatic is inherent to a state-machine Update.
func (m logsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.close()
			return m, tea.Quit
		}
		if msg.Type == tea.KeyEsc {
			if m.step == stepLogsInspect {
				m.step = stepLogsStreaming
				return m, nil
			}
			m.close()
			return m, tea.Quit
		}
		if m.step == stepLogsInspect {
			var cmd tea.Cmd
			m.inspectVP, cmd = m.inspectVP.Update(msg)
			return m, cmd
		}
		if m.step == stepLogsStreaming {
			if msg.Type == tea.KeyEnter {
				return m.openInspect()
			}
			if msg.Type == tea.KeySpace {
				m.paused = !m.paused
				if m.ready && len(m.lines) > 0 {
					m.vp.SetContent(m.vpContent())
				}
				return m, nil
			}
			if msg.String() == "t" {
				m.localTime = !m.localTime
				m.lines = m.reformatLines()
				if m.ready && len(m.lines) > 0 {
					atBottom := m.vp.AtBottom()
					m.vp.SetContent(m.vpContent())
					if atBottom {
						m.vp.GotoBottom()
					}
				}
				return m, nil
			}
			if msg.Type == tea.KeyUp || msg.Type == tea.KeyDown {
				if msg.Type == tea.KeyUp {
					m.cursor--
				} else {
					m.cursor++
				}
				if m.cursor < 0 {
					m.cursor = 0
				}
				if m.cursor >= len(m.rawEntries) {
					m.cursor = len(m.rawEntries) - 1
				}
				m.syncViewportToCursor()
				if m.ready && len(m.lines) > 0 {
					m.vp.SetContent(m.vpContent())
				}

				if m.cursor == 0 && !m.fetchingOlder && !m.noOlderEntries && m.firstTS != "" {
					m.fetchingOlder = true
					return m, m.fetchOlderLogs()
				}
				return m, nil
			}
			if m.ready && (msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown || msg.Type == tea.KeyHome || msg.Type == tea.KeyEnd) {
				var cmd tea.Cmd
				m.vp, cmd = m.vp.Update(msg)
				m.syncCursorToViewport()
				return m, cmd
			}
		}

	case tea.WindowSizeMsg:
		const headerLines = 2 // logHeader row + top scroll indicator
		const footerLines = 3 // bottom scroll indicator + blank + hints
		vpH := msg.Height - headerLines - footerLines
		if vpH < 1 {
			vpH = 1
		}
		if !m.ready {
			m.vp = viewport.New(msg.Width, vpH)
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = vpH
		}
		if !m.inspectVPReady {
			m.inspectVP = viewport.New(msg.Width, vpH)
			m.inspectVPReady = true
		} else {
			m.inspectVP.Width = msg.Width
			m.inspectVP.Height = vpH
		}
		if len(m.lines) > 0 {
			m.vp.SetContent(m.vpContent())
		}
		if m.step == stepLogsInspect && m.inspectVPReady {
			m.inspectVP.SetContent(m.renderInspectView())
		}
		return m, nil

	case clockTickMsg:
		return m, clockTick()

	case tickMsg:
		if !m.paused {
			return m, m.fetchLogs()
		}
		return m, nil

	case logsResultMsg:
		return m.handleResult(msg)

	case olderLogsResultMsg:
		return m.handleOlderResult(msg)

	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m logsModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepLogsLoading:
		b.WriteString(m.spinner.View() + " Fetching logs...")
	case stepLogsStreaming:
		if !m.ready {
			b.WriteString(m.spinner.View() + " Initializing...")
		} else {
			b.WriteString(logHeader() + "\n")

			if m.fetchingOlder {
				b.WriteString(tui.StyleMuted.Render("  ↑ Loading older entries...") + "\n")
			} else if m.noOlderEntries {
				b.WriteString(tui.StyleMuted.Render("  ↑ No older entries") + "\n")
			} else if len(m.lines) > 0 && !m.vp.AtTop() {
				b.WriteString(tui.StyleMuted.Render("  ↑") + "\n")
			} else {
				b.WriteString("\n")
			}

			if len(m.lines) == 0 {
				placeholder := "No logs yet. Make a request to your app to see logs here."
				if m.paused {
					placeholder = "No logs yet. [PAUSED] Make a request to see logs here."
				}
				b.WriteString(tui.StyleMuted.Render(placeholder))
			} else {
				b.WriteString(m.vp.View())
			}

			if len(m.lines) > 0 && !m.vp.AtBottom() {
				b.WriteString("\n" + tui.StyleMuted.Render("  ↓"))
			} else {
				b.WriteString("\n")
			}
		}
	case stepLogsInspect:
		if !m.inspectVPReady {
			b.WriteString("Initializing inspect view...")
		} else {
			b.WriteString(m.inspectVP.View())
		}
	}

	// Footer: key hints + scroll position
	timeLabel := "UTC"
	if m.localTime {
		timeLabel = "local"
	}
	hints := fmt.Sprintf("SPACE: pause | ↑↓ PgUp/PgDn: scroll | Enter: inspect | t: time [%s] | Ctrl+C: exit", timeLabel)
	if m.paused {
		hints = fmt.Sprintf("SPACE: resume | ↑↓ PgUp/PgDn: scroll | Enter: inspect | t: time [%s] | Ctrl+C: exit | [PAUSED]", timeLabel)
	}
	scrollInfo := ""
	if m.ready && len(m.lines) > 0 {
		scrollInfo = fmt.Sprintf(" %3.f%%", m.vp.ScrollPercent()*100)
	}

	now := time.Now()
	if m.localTime {
		now = time.Now().Local()
	}
	clock := now.Format("15:04:05")
	left := tui.StyleMuted.Render(hints + scrollInfo)
	right := tui.StyleMuted.Render(clock)
	gap := m.vp.Width - len(hints+scrollInfo) - len(clock)
	if gap < 1 {
		gap = 1
	}
	b.WriteString("\n" + left + strings.Repeat(" ", gap) + right)

	return b.String()
}

func (m logsModel) handleResult(msg logsResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		code := status.Code(msg.err)
		switch code {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Internal:
			m.retryCount++
			if m.retryCount >= 5 {
				m.err = friendlyLogsError(msg.err)
				m.close()
				return m, tea.Quit
			}
			backoff := 5 * time.Second * (1 << (m.retryCount - 1))
			return m, tick(backoff)
		default:
			m.err = friendlyLogsError(msg.err)
			m.close()
			return m, tea.Quit
		}
	}

	m.retryCount = 0
	if m.step == stepLogsLoading {
		m.step = stepLogsStreaming
	}

	entries := msg.resp.Entries
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ts < entries[j].Ts })

	for _, entry := range entries {
		key := entry.Ts + ":" + entry.NodeId
		if _, dup := m.seen[key]; dup {
			continue
		}
		m.seen[key] = struct{}{}

		if entry.Ts > m.lastTS {
			m.lastTS = entry.Ts
		}

		if m.firstTS == "" || entry.Ts < m.firstTS {
			m.firstTS = entry.Ts
		}

		m.rawEntries = append(m.rawEntries, entry)
		m.totalEntries++

	}

	m.lines = m.reformatLines()

	if m.cursor >= len(m.rawEntries) {
		m.cursor = len(m.rawEntries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	if m.ready && len(m.lines) > 0 {
		atBottom := m.vp.AtBottom()
		m.vp.SetContent(m.vpContent())
		if atBottom {
			m.vp.GotoBottom()
		}
	}

	if m.paused {
		return m, nil
	}
	return m, tick(3 * time.Second)
}

func (m logsModel) handleOlderResult(msg olderLogsResultMsg) (tea.Model, tea.Cmd) {
	m.fetchingOlder = false

	if msg.err != nil {
		return m, nil
	}

	entries := msg.resp.Entries
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ts < entries[j].Ts })

	var newEntries []*pb.RequestLogEntry
	for _, entry := range entries {
		key := entry.Ts + ":" + entry.NodeId
		if _, dup := m.seen[key]; dup {
			continue
		}
		m.seen[key] = struct{}{}
		newEntries = append(newEntries, entry)
	}

	if len(newEntries) == 0 {
		m.noOlderEntries = true
		return m, nil
	}

	m.cursor += len(newEntries)
	m.rawEntries = append(newEntries, m.rawEntries...)
	m.totalEntries += len(newEntries)
	m.firstTS = newEntries[0].Ts

	m.lines = m.reformatLines()

	if m.ready && len(m.lines) > 0 {
		m.vp.SetContent(m.vpContent())
		m.vp.SetYOffset(m.cursor - m.vp.Height/2)
	}

	return m, nil
}

// syncViewportToCursor adjusts the viewport so the cursor row is visible.
func (m *logsModel) syncViewportToCursor() {
	if !m.ready || len(m.lines) == 0 {
		return
	}
	cursorLine := m.cursor
	top := m.vp.YOffset
	bottom := top + m.vp.Height - 1
	if cursorLine < top {
		m.vp.SetYOffset(cursorLine)
	} else if cursorLine > bottom {
		m.vp.SetYOffset(cursorLine - m.vp.Height + 1)
	}
}

// syncCursorToViewport moves the cursor to stay within the visible viewport.
func (m *logsModel) syncCursorToViewport() {
	if !m.ready || len(m.lines) == 0 {
		return
	}
	top := m.vp.YOffset
	bottom := top + m.vp.Height - 1
	if bottom >= len(m.lines) {
		bottom = len(m.lines) - 1
	}
	if m.cursor < top {
		m.cursor = top
	} else if m.cursor > bottom {
		m.cursor = bottom
	}
}

func (m logsModel) vpContent() string {
	if len(m.lines) == 0 {
		return ""
	}
	status := tui.StyleMuted.Render("── waiting for new requests ──")
	if m.paused {
		status = tui.StyleMuted.Render("── paused · press SPACE to resume ──")
	}

	highlight := lipgloss.NewStyle().Background(tui.ColorBorder)

	rendered := make([]string, len(m.lines))
	for i, line := range m.lines {
		if i == m.cursor {
			rendered[i] = highlight.Render(line)
		} else {
			rendered[i] = line
		}
	}

	halfPad := m.vp.Height / 2
	if halfPad < 1 {
		halfPad = 1
	}
	return strings.Join(rendered, "\n") + "\n" + status + strings.Repeat("\n", halfPad-1)
}

// openInspect switches to the inspect step to show details for the cursor entry.
func (m logsModel) openInspect() (logsModel, tea.Cmd) {
	m.step = stepLogsInspect
	if m.inspectVPReady {
		m.inspectVP.SetContent(m.renderInspectView())
		m.inspectVP.GotoTop()
	}
	return m, nil
}

func (m logsModel) renderInspectView() string {
	if m.cursor < 0 || m.cursor >= len(m.rawEntries) {
		return tui.StyleMuted.Render("No entry selected.")
	}

	entry := m.rawEntries[m.cursor]
	f := parseLogPayload(entry.Payload)
	ts := formatTS(entry.Ts, m.localTime)

	labelWidth := 12 // "User-Agent" is the longest label with colon
	indent := strings.Repeat(" ", labelWidth+2)

	rows := []struct{ label, value string }{
		{"Timestamp", ts},
		{"Hostname", f.Host},
		{"IP", f.ClientIP},
		{"Method", f.Method},
		{"Path", f.Path},
		{"Status", fmt.Sprintf("%d", f.Status)},
		{"Duration", fmt.Sprintf("%dms", f.DurationMs)},
		{"Cache", f.CacheStatus},
		{"Node", entry.NodeId},
		{"User-Agent", f.UserAgent},
	}

	var b strings.Builder
	b.WriteString("\n")
	for _, row := range rows {
		label := fmt.Sprintf("%-*s", labelWidth, row.label+":")
		value := row.value
		if value == "" || value == "0" || value == "0ms" {
			value = "-"
		}
		wrapped := wrapText(value, m.inspectVP.Width-labelWidth-4)
		lines := strings.Split(wrapped, "\n")
		for j, line := range lines {
			if j == 0 {
				b.WriteString("  " + label + " " + line + "\n")
			} else {
				b.WriteString("  " + indent + " " + line + "\n")
			}
		}
	}
	b.WriteString("\n")

	content := b.String()
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tui.ColorBorder).
		Padding(0, 1).
		Width(m.inspectVP.Width - 4)

	header := tui.StyleTitle.Render(" Request Detail ")
	footer := tui.StyleMuted.Render(" [Esc] back")

	return header + "\n" + borderStyle.Render(content) + "\n" + footer
}

func (m logsModel) fetchLogs() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return logsResultMsg{err: fmt.Errorf("connect to server: connection not established")}
		}

		since := m.lastTS
		if since == "" {
			since = logsSince
		}

		ctx := authContext(m.ctx, m.cfg.Token)
		resp, err := m.client.GetRequestLogs(ctx, &pb.GetRequestLogsRequest{
			SlugName: m.slug,
			Domain:   m.domain,
			Since:    since,
			Limit:    logsLimit,
		})
		if err != nil {
			return logsResultMsg{err: fmt.Errorf("get logs: %w", err)}
		}
		return logsResultMsg{resp: resp}
	}
}

func (m logsModel) fetchOlderLogs() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return olderLogsResultMsg{err: fmt.Errorf("connect to server: connection not established")}
		}

		ctx := authContext(m.ctx, m.cfg.Token)
		resp, err := m.client.GetRequestLogs(ctx, &pb.GetRequestLogsRequest{
			SlugName: m.slug,
			Domain:   m.domain,
			Since:    logsSince,
			Until:    m.firstTS,
			Limit:    logsLimit,
		})
		if err != nil {
			return olderLogsResultMsg{err: fmt.Errorf("get older logs: %w", err)}
		}
		return olderLogsResultMsg{resp: resp}
	}
}

// reformatLines rebuilds the formatted lines cache from rawEntries using the
// current localTime setting.
func (m logsModel) reformatLines() []string {
	lines := make([]string, len(m.rawEntries))
	for i, entry := range m.rawEntries {
		lines[i] = formatLine(entry, logsFormat, m.localTime)
	}
	return lines
}
