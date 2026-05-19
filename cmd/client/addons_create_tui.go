package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

type addonsCreateModel struct {
	spinner spinner.Model
	cfg     clientConfig
	ctx     context.Context
	addonID string
	appSlug string
	domain  string
	plan    string
	config  map[string]string
	message string
	done    bool
	err     error
}

type addonsCreateResultMsg struct {
	config  map[string]string
	message string
	err     error
}

func newAddonsCreateModel(addonID, appSlug, domain, plan string, cfg clientConfig, ctx context.Context) addonsCreateModel {
	return addonsCreateModel{
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		addonID: addonID,
		appSlug: appSlug,
		domain:  domain,
		plan:    plan,
	}
}

func (m addonsCreateModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return addonsCreateResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.create)
}

func (m addonsCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	case addonsCreateResultMsg:
		m.done = true
		m.err = msg.err
		m.config = msg.config
		m.message = msg.message
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m addonsCreateModel) View() string {
	if m.done {
		if m.err != nil {
			return tui.ErrorIcon(m.err.Error())
		}
		return tui.SuccessIcon(fmt.Sprintf("Provisioned %s", m.addonID))
	}
	return m.spinner.View() + fmt.Sprintf(" Provisioning %s...", m.addonID)
}

func (m addonsCreateModel) create() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return addonsCreateResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.AddonsCreate(ctx, &pb.AddonsCreateRequest{
		AddonId: m.addonID,
		AppSlug: m.appSlug,
		Domain:  m.domain,
		Plan:    m.plan,
	})
	if err != nil {
		return addonsCreateResultMsg{err: fmt.Errorf("create addon: %w", err)}
	}
	return addonsCreateResultMsg{config: resp.Config, message: resp.Message}
}
