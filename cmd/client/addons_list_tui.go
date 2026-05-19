package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

type addonsListModel struct {
	spinner spinner.Model
	table   table.Model
	cfg     clientConfig
	ctx     context.Context
	addonID string // empty = list all; non-empty = info for one addon
	appSlug string
	domain  string
	message string // provider setup message (info view only)
	done    bool
	empty   bool // true when the server returned no addons
	err     error
}

type addonsListResultMsg struct {
	addons []*pb.AddonsListResponse_AddonResource
	err    error
}

func newAddonsListModel(addonID, appSlug, domain string, cfg clientConfig, ctx context.Context) addonsListModel {
	return addonsListModel{
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		addonID: addonID,
		appSlug: appSlug,
		domain:  domain,
	}
}

func (m addonsListModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return addonsListResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetch)
}

func (m addonsListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	case addonsListResultMsg:
		return m.handleResult(msg)
	}
	if !m.done {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m addonsListModel) View() string {
	if !m.done {
		label := "Fetching addons..."
		if m.addonID != "" {
			label = fmt.Sprintf("Fetching %s...", m.addonID)
		}
		return m.spinner.View() + " " + label
	}

	if m.err != nil {
		return tui.ErrorIcon(m.err.Error())
	}

	if len(m.table.Rows()) == 0 {
		if m.addonID != "" {
			return tui.StyleMuted.Render(fmt.Sprintf("Addon %s is not attached to this app.", m.addonID))
		}
		return tui.StyleMuted.Render("No addons attached. Run 'fyra addons create <addon-id>' to add one.")
	}

	var b strings.Builder
	if m.addonID != "" {
		b.WriteString(tui.StyleTitle.Render("Addon: "+m.addonID) + "\n\n")
	}
	b.WriteString(m.table.View())
	if m.message != "" {
		b.WriteString("\n\n")
		b.WriteString(tui.StyleTitle.Render("Setup"))
		b.WriteString("\n")
		b.WriteString(m.message)
	}
	return b.String()
}

func (m addonsListModel) handleResult(msg addonsListResultMsg) (tea.Model, tea.Cmd) {
	m.done = true
	if msg.err != nil {
		m.err = msg.err
		return m, tea.Quit
	}
	if len(msg.addons) == 0 {
		m.empty = true
		return m, tea.Quit
	}

	tableStyles := tui.NewTableStyles()

	if m.addonID != "" {
		// Info view: show config vars for a single addon.
		ar := msg.addons[0]
		m.message = ar.Message
		columns := []table.Column{
			{Title: "Key", Width: 30},
			{Title: "Value", Width: 50},
		}
		rows := make([]table.Row, 0, len(ar.Config)+3)
		rows = append(rows,
			table.Row{"Plan", ar.Plan},
			table.Row{"Status", ar.Status},
			table.Row{"Attached", ar.CreatedAt[:10]},
		)
		for k, v := range ar.Config {
			rows = append(rows, table.Row{k, v})
		}
		m.table = table.New(
			table.WithColumns(columns),
			table.WithRows(rows),
			table.WithFocused(false),
			table.WithStyles(tableStyles),
		)
	} else {
		// List view: one row per addon.
		columns := []table.Column{
			{Title: "Addon", Width: 16},
			{Title: "Plan", Width: 12},
			{Title: "Status", Width: 14},
			{Title: "Attached", Width: 12},
		}
		rows := make([]table.Row, len(msg.addons))
		for i, ar := range msg.addons {
			created := ar.CreatedAt
			if len(created) >= 10 {
				created = created[:10]
			}
			rows[i] = table.Row{ar.AddonId, ar.Plan, ar.Status, created}
		}
		m.table = table.New(
			table.WithColumns(columns),
			table.WithRows(rows),
			table.WithFocused(false),
			table.WithStyles(tableStyles),
		)
	}

	return m, tea.Quit
}

func (m addonsListModel) fetch() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return addonsListResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.AddonsList(ctx, &pb.AddonsListRequest{
		AppSlug: m.appSlug,
		Domain:  m.domain,
		AddonId: m.addonID,
	})
	if err != nil {
		return addonsListResultMsg{err: fmt.Errorf("list addons: %w", err)}
	}
	return addonsListResultMsg{addons: resp.Addons}
}
