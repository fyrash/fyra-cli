package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

type addonsOpenStep int

const (
	stepOpenLoading addonsOpenStep = iota
	stepOpenDone
)

type addonsOpenModel struct {
	step        addonsOpenStep
	spinner     spinner.Model
	cfg         clientConfig
	ctx         context.Context
	addonID     string
	appSlug     string
	domain      string
	redirectURL string
	err         error
}

type addonsOpenResultMsg struct {
	redirectURL string
	err         error
}

func newAddonsOpenModel(addonID, appSlug, domain string, cfg clientConfig, ctx context.Context) addonsOpenModel {
	return addonsOpenModel{
		step:    stepOpenLoading,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		addonID: addonID,
		appSlug: appSlug,
		domain:  domain,
	}
}

func (m addonsOpenModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return addonsOpenResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchOpen)
}

func (m addonsOpenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
	case addonsOpenResultMsg:
		m.step = stepOpenDone
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.redirectURL = msg.redirectURL
		return m, tea.Quit
	}

	var cmd tea.Cmd
	if m.step == stepOpenLoading {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m addonsOpenModel) View() string {
	if m.step == stepOpenLoading {
		return m.spinner.View() + " Requesting dashboard URL..."
	}
	return ""
}

func (m addonsOpenModel) fetchOpen() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return addonsOpenResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.AddonsDashboard(ctx, &pb.AddonsDashboardRequest{
		AddonId: m.addonID,
		AppSlug: m.appSlug,
		Domain:  m.domain,
	})
	if err != nil {
		// Translate Unimplemented (old server without AddonsDashboard) into a
		// friendlier message so the user knows to upgrade rather than seeing
		// a raw gRPC error.
		if status.Code(err) == codes.Unimplemented {
			return addonsOpenResultMsg{err: fmt.Errorf("this fyra server does not support 'addons open' — upgrade your fyra server")}
		}
		return addonsOpenResultMsg{err: fmt.Errorf("open dashboard: %w", err)}
	}
	return addonsOpenResultMsg{redirectURL: resp.RedirectUrl}
}
