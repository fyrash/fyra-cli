package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// linkStep represents the current step in the link flow.
type linkStep int

const (
	stepLinkLoading linkStep = iota
	stepLinkChoose
	stepLinkDone
)

// linkModel is the Bubble Tea model for the link command.
type linkModel struct {
	step         linkStep
	spinner      spinner.Model
	input        textinput.Model
	cfg          clientConfig
	ctx          context.Context
	slug         string // from CLI arg
	matches      []*pb.AppInfo
	domain       string
	customDomain string
	createdAt    string
	err          error
	quitting     bool
}

// linkResultMsg carries the result of the ListApps + filter call.
type linkResultMsg struct {
	matches []*pb.AppInfo
	err     error
}

func newLinkModel(slug string, cfg clientConfig, ctx context.Context) linkModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 3
	ti.Width = 10

	return linkModel{
		step:    stepLinkLoading,
		spinner: tui.NewSpinner(),
		input:   ti,
		cfg:     cfg,
		ctx:     ctx,
		slug:    slug,
	}
}

func (m linkModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return linkResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchMatchingApps)
}

func (m linkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			m.step = stepLinkDone
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		case tea.KeyEnter:
			if m.step == stepLinkChoose {
				return m.handleChoice()
			}
		}
	case linkResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	switch m.step {
	case stepLinkLoading:
		m.spinner, cmd = m.spinner.Update(msg)
	case stepLinkChoose:
		m.input, cmd = m.input.Update(msg)
	}
	return m, cmd
}

func (m linkModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepLinkLoading:
		b.WriteString(m.spinner.View() + fmt.Sprintf(" Looking up %s...", m.slug))

	case stepLinkChoose:
		b.WriteString(tui.StyleTitle.Render("Multiple apps found") + "\n\n")
		for i, a := range m.matches {
			url := a.Url
			if url == "" {
				url = fmt.Sprintf("%s.%s", a.SlugName, a.Domain)
			}
			fmt.Fprintf(&b, "  %d. %s\n", i+1, url)
		}
		b.WriteString("\n")
		b.WriteString(m.input.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter number to link · esc to cancel"))

	case stepLinkDone:
		// Output printed by link.go after tui.Run() returns.
	}

	return b.String()
}

// handleResult processes the ListApps response.
func (m linkModel) handleResult(msg linkResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.step = stepLinkDone
		m.err = msg.err
		return m, tea.Quit
	}

	switch len(msg.matches) {
	case 0:
		m.step = stepLinkDone
		m.err = fmt.Errorf("no app found with slug %q — check with '%s list'", m.slug, binaryName)
		return m, tea.Quit
	case 1:
		return m.selectApp(msg.matches[0])
	default:
		m.matches = msg.matches
		m.input.Focus()
		m.step = stepLinkChoose
		return m, textinput.Blink
	}
}

// handleChoice processes the user's numeric selection when multiple matches exist.
func (m linkModel) handleChoice() (tea.Model, tea.Cmd) {
	m.err = nil
	raw := strings.TrimSpace(m.input.Value())
	if raw == "" {
		m.err = fmt.Errorf("enter a number")
		return m, nil
	}
	idx, err := strconv.Atoi(raw)
	if err != nil || idx < 1 || idx > len(m.matches) {
		m.err = fmt.Errorf("enter a number 1-%d", len(m.matches))
		return m, nil
	}
	return m.selectApp(m.matches[idx-1])
}

// selectApp finalises the link for a specific app.
func (m linkModel) selectApp(app *pb.AppInfo) (tea.Model, tea.Cmd) {
	m.domain = app.Domain
	m.customDomain = app.CustomDomain
	m.createdAt = app.CreatedAt
	if app.SlugName != "" {
		m.slug = app.SlugName
	}
	m.step = stepLinkDone
	return m, tea.Quit
}

// fetchMatchingApps calls ListApps and filters by slug.
func (m linkModel) fetchMatchingApps() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return linkResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.ListApps(ctx, &pb.ListAppsRequest{})
	if err != nil {
		return linkResultMsg{err: fmt.Errorf("list apps: %w", err)}
	}

	var matches []*pb.AppInfo
	for _, a := range resp.Apps {
		if a.SlugName == m.slug {
			matches = append(matches, a)
		}
	}
	return linkResultMsg{matches: matches}
}
