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

// whoamiStep represents the current step in the whoami flow.
type whoamiStep int

const (
	stepWhoamiLoading whoamiStep = iota
	stepWhoamiDone
)

// whoamiModel is the Bubble Tea model for the whoami command.
type whoamiModel struct {
	step      whoamiStep
	spinner   spinner.Model
	cfg       clientConfig
	ctx       context.Context
	email     string
	confirmed bool
	err       error
}

// whoamiResultMsg carries the result of the GRPC WhoAmI call.
type whoamiResultMsg struct {
	email     string
	confirmed bool
	err       error
}

func newWhoamiModel(cfg clientConfig, ctx context.Context) whoamiModel {
	return whoamiModel{
		step:    stepWhoamiLoading,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
	}
}

func (m whoamiModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return whoamiResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchWhoami)
}

func (m whoamiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}

	case whoamiResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	if m.step == stepWhoamiLoading {
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m whoamiModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepWhoamiLoading:
		b.WriteString(m.spinner.View() + " Fetching account info...")

	case stepWhoamiDone:
		// Output printed by whoami.go after tui.Run() returns,
		// to avoid the inline renderer clearing it on tea.Quit.
	}

	return b.String()
}

// fetchWhoami is a tea.Cmd that calls the GRPC WhoAmI RPC.
func (m whoamiModel) fetchWhoami() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return whoamiResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.WhoAmI(ctx, &pb.WhoAmIRequest{})
	if err != nil {
		return whoamiResultMsg{err: fmt.Errorf("whoami: %w", err)}
	}
	return whoamiResultMsg{email: resp.Email, confirmed: resp.Confirmed}
}

// handleResult processes the GRPC response.
func (m whoamiModel) handleResult(msg whoamiResultMsg) (tea.Model, tea.Cmd) {
	m.step = stepWhoamiDone
	if msg.err != nil {
		m.err = msg.err
		return m, tea.Quit
	}
	m.email = msg.email
	m.confirmed = msg.confirmed
	return m, tea.Quit
}
