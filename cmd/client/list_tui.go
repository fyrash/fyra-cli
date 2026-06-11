package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// listStep represents the current step in the list flow.
type listStep int

const (
	stepListLoading listStep = iota
	stepListDone
)

// listModel is the Bubble Tea model for the list command.
type listModel struct {
	step    listStep
	spinner spinner.Model
	table   table.Model
	cfg     clientConfig
	ctx     context.Context
	err     error
}

// listResultMsg carries the result of the GRPC ListApps call.
type listResultMsg struct {
	apps []*pb.AppInfo
	err  error
}

func newListModel(cfg clientConfig, ctx context.Context) listModel {
	return listModel{
		step:    stepListLoading,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
	}
}

func (m listModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return listResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchApps)
}

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
		if m.step == stepListDone {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		if m.step == stepListDone {
			m.table.SetWidth(msg.Width)
			m.table.SetHeight(msg.Height - 4)
		}

	case listResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	if m.step == stepListLoading {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m listModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepListLoading:
		b.WriteString(m.spinner.View() + " Fetching apps...")

	case stepListDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else if len(m.table.Rows()) == 0 {
			b.WriteString(tui.StyleMuted.Render(
				fmt.Sprintf("No apps yet. Run '%s create' to create one.", binaryName),
			))
		} else {
			b.WriteString(m.table.View())
		}
	}

	return b.String()
}

// fetchApps is a tea.Cmd that calls the GRPC ListApps RPC.
func (m listModel) fetchApps() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return listResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.ListApps(ctx, &pb.ListAppsRequest{})
	if err != nil {
		return listResultMsg{err: fmt.Errorf("list apps: %w", err)}
	}
	return listResultMsg{apps: resp.Apps}
}

// handleResult processes the GRPC response and builds the table.
func (m listModel) handleResult(msg listResultMsg) (tea.Model, tea.Cmd) {
	m.step = stepListDone

	if msg.err != nil {
		if isEmailNotConfirmed(msg.err) {
			m.err = fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
		} else {
			m.err = msg.err
		}
		return m, tea.Quit
	}

	if len(msg.apps) == 0 {
		return m, tea.Quit
	}

	columns := []table.Column{
		{Title: "Slug", Width: 24},
		{Title: "URL", Width: 40},
		{Title: "Custom Domain", Width: 30},
		{Title: "Local Path", Width: 30},
		{Title: "Created", Width: 12},
	}

	// Load local app index for path lookups.
	index, _ := appindex.Load()

	rows := make([]table.Row, len(msg.apps))
	for i, a := range msg.apps {
		url := a.Url
		if url == "" {
			url = "(not deployed)"
		}
		domain := a.CustomDomain
		if domain == "" {
			domain = "-"
		}
		localPath := "-"
		if p, ok := index[a.SlugName]; ok {
			localPath = p.Path
		}
		created := a.CreatedAt
		if len(created) >= 10 {
			created = created[:10]
		}
		rows[i] = table.Row{a.SlugName, url, domain, localPath, created}
	}

	m.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithHeight(len(rows)+1),
		table.WithFocused(false),
		table.WithStyles(tui.NewTableStyles()),
	)

	return m, tea.Quit
}
