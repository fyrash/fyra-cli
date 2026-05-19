package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// logoutStep represents the current step in the logout flow.
type logoutStep int

const (
	stepLogoutSubmitting logoutStep = iota
	stepLogoutDone
)

// logoutModel is the Bubble Tea model for the logout command.
type logoutModel struct {
	step    logoutStep
	spinner spinner.Model
	cfg     clientConfig
	ctx     context.Context
	err     error
}

// logoutResultMsg carries the result of the GRPC Logout call.
type logoutResultMsg struct{ err error }

func newLogoutModel(cfg clientConfig, ctx context.Context) logoutModel {
	return logoutModel{
		step:    stepLogoutSubmitting,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
	}
}

func (m logoutModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return logoutResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.performLogout)
}

func (m logoutModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}

	case logoutResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	if m.step == stepLogoutSubmitting {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m logoutModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepLogoutSubmitting:
		b.WriteString(m.spinner.View() + " Logging out...")
	case stepLogoutDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(tui.SuccessIcon("Logged out."))
		}
	}

	return b.String()
}

// performLogout is a tea.Cmd that calls the GRPC Logout RPC.
func (m logoutModel) performLogout() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return logoutResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	if _, err := client.Logout(ctx, &pb.LogoutRequest{}); err != nil {
		return logoutResultMsg{err: fmt.Errorf("logout: %w", err)}
	}
	return logoutResultMsg{}
}

// handleResult processes the GRPC response and clears the token on success.
func (m logoutModel) handleResult(msg logoutResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.step = stepLogoutDone
		return m, tea.Quit
	}

	m.cfg.Token = ""
	if err := saveConfig(m.cfg); err != nil {
		m.err = fmt.Errorf("clear config: %w", err)
		m.step = stepLogoutDone
		return m, tea.Quit
	}

	m.step = stepLogoutDone
	return m, tea.Quit
}
