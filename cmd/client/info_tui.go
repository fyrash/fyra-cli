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

type infoStep int

const (
	stepInfoLoading infoStep = iota
	stepInfoDone
)

type infoModel struct {
	step    infoStep
	spinner spinner.Model
	cfg     clientConfig
	ctx     context.Context
	slug    string
	domain  string
	table   table.Model
	err     error
}

type infoAppResultMsg struct {
	app    *pb.AppInfo
	addons []*pb.AddonsListResponse_AddonResource
	err    error
}

func newInfoModel(slug, domain string, cfg clientConfig, ctx context.Context) infoModel {
	return infoModel{
		step:    stepInfoLoading,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		slug:    slug,
		domain:  domain,
	}
}

func (m infoModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return infoAppResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchInfo)
}

func (m infoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
		if m.step == stepInfoDone {
			return m, tea.Quit
		}

	case infoAppResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	if m.step == stepInfoLoading {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m infoModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepInfoLoading:
		b.WriteString(m.spinner.View() + " Fetching app info...")

	case stepInfoDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(m.table.View())
		}
	}

	return b.String()
}

func (m infoModel) fetchInfo() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return infoAppResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)

	// Fetch app list and addons in parallel.
	type appResult struct {
		apps   []*pb.AppInfo
		addons []*pb.AddonsListResponse_AddonResource
		err    error
	}

	appCh := make(chan appResult, 1)
	addonCh := make(chan appResult, 1)

	go func() {
		resp, err := client.ListApps(ctx, &pb.ListAppsRequest{})
		if err != nil {
			appCh <- appResult{err: fmt.Errorf("list apps: %w", err)}
			return
		}
		appCh <- appResult{apps: resp.Apps}
	}()

	go func() {
		resp, err := client.AddonsList(ctx, &pb.AddonsListRequest{
			AppSlug: m.slug,
			Domain:  m.domain,
		})
		if err != nil {
			// Addons failure is non-fatal — just show no addons.
			addonCh <- appResult{}
			return
		}
		addonCh <- appResult{addons: resp.Addons}
	}()

	ar := <-appCh
	if ar.err != nil {
		return infoAppResultMsg{err: ar.err}
	}

	// Find the matching app.
	var app *pb.AppInfo
	for _, a := range ar.apps {
		if a.SlugName == m.slug && (m.domain == "" || a.Domain == m.domain) {
			app = a
			break
		}
	}
	if app == nil {
		return infoAppResultMsg{err: fmt.Errorf("app '%s' not found. It may have been deleted.", m.slug)}
	}

	adr := <-addonCh
	return infoAppResultMsg{app: app, addons: adr.addons}
}

func (m infoModel) handleResult(msg infoAppResultMsg) (tea.Model, tea.Cmd) {
	m.step = stepInfoDone

	if msg.err != nil {
		if isEmailNotConfirmed(msg.err) {
			m.err = fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
		} else {
			m.err = msg.err
		}
		return m, tea.Quit
	}

	app := msg.app

	// Build a key-value table for app details + addons.
	columns := []table.Column{
		{Title: "", Width: 16},
		{Title: "", Width: 60},
	}

	var rows []table.Row

	url := app.Url
	if url == "" {
		url = "(not deployed)"
	} else {
		url = "https://" + url
	}
	customDomain := app.CustomDomain
	if customDomain == "" {
		customDomain = "-"
	}
	created := app.CreatedAt
	if len(created) >= 10 {
		created = created[:10]
	}

	rows = append(rows,
		table.Row{"App", app.SlugName},
		table.Row{"URL", url},
		table.Row{"Custom Domain", customDomain},
		table.Row{"Domain Zone", app.Domain},
		table.Row{"Created", created},
	)

	if len(msg.addons) > 0 {
		rows = append(rows, table.Row{"Addons", ""})
		for _, a := range msg.addons {
			rows = append(rows, table.Row{"  " + a.AddonId, a.Plan + " (" + a.Status + ")"})
		}
	}

	m.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithStyles(tui.NewTableStyles()),
	)

	return m, tea.Quit
}
