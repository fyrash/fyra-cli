package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

type addonsDestroyModel struct {
	spinner spinner.Model
	cfg     clientConfig
	ctx     context.Context
	addonID string
	appSlug string
	domain  string
	done    bool
	err     error
}

type addonsDestroyResultMsg struct {
	err error
}

func newAddonsDestroyModel(addonID, appSlug, domain string, cfg clientConfig, ctx context.Context) addonsDestroyModel {
	return addonsDestroyModel{
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		addonID: addonID,
		appSlug: appSlug,
		domain:  domain,
	}
}

func (m addonsDestroyModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return addonsDestroyResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.destroy)
}

func (m addonsDestroyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	case addonsDestroyResultMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m addonsDestroyModel) View() string {
	if m.done {
		if m.err != nil {
			return tui.ErrorIcon(m.err.Error())
		}
		return tui.SuccessIcon(fmt.Sprintf("Deprovisioned %s", m.addonID))
	}
	return m.spinner.View() + fmt.Sprintf(" Deprovisioning %s...", m.addonID)
}

func (m addonsDestroyModel) destroy() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return addonsDestroyResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	_, err = client.AddonsDestroy(ctx, &pb.AddonsDestroyRequest{
		AddonId: m.addonID,
		AppSlug: m.appSlug,
		Domain:  m.domain,
	})
	if err != nil {
		return addonsDestroyResultMsg{err: fmt.Errorf("destroy addon: %w", err)}
	}
	return addonsDestroyResultMsg{}
}
