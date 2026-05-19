package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// loginStep represents the current step in the login flow.
type loginStep int

const (
	stepLoginEmail loginStep = iota
	stepLoginPassword
	stepLoginSubmitting
	stepLoginDone
)

// loginModel is the Bubble Tea model for the login command.
type loginModel struct {
	step       loginStep
	email      textinput.Model
	password   textinput.Model
	spinner    spinner.Model
	cfg        clientConfig
	ctx        context.Context
	err        error
	quitting   bool
}

// loginResultMsg carries the result of the GRPC Login call.
type loginResultMsg struct {
	token string
	err   error
}

func newLoginModel(cfg clientConfig, ctx context.Context) loginModel {
	email := tui.NewEmailInput()
	password := tui.NewPasswordInput()
	s := tui.NewSpinner()

	return loginModel{
		step:     stepLoginEmail,
		email:    email,
		password: password,
		spinner:  s,
		cfg:      cfg,
		ctx:      ctx,
	}
}

func (m loginModel) Init() tea.Cmd {
	return textinput.Blink
}

//nolint:revive // cyclomatic is inherent to a state-machine Update.
func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			if m.step == stepLoginSubmitting {
				m.step = stepLoginDone
				m.err = fmt.Errorf("cancelled")
			}
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleEnter()
		}

	case loginResultMsg:
		return m.handleResult(msg)
	}

	// Delegate to the active sub-model.
	var cmd tea.Cmd
	switch m.step {
	case stepLoginEmail:
		m.email, cmd = m.email.Update(msg)
	case stepLoginPassword:
		m.password, cmd = m.password.Update(msg)
	case stepLoginSubmitting:
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

func (m loginModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepLoginEmail:
		b.WriteString(tui.StyleTitle.Render("Log in to fyra.sh"))
		b.WriteString("\n\n")
		b.WriteString("Email:\n")
		b.WriteString(m.email.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepLoginPassword:
		b.WriteString(tui.StyleTitle.Render("Log in to fyra.sh"))
		b.WriteString("\n\n")
		b.WriteString(tui.StyleMuted.Render("Email: "+m.email.Value()))
		b.WriteString("\n\nPassword:\n")
		b.WriteString(m.password.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to log in · esc to cancel"))

	case stepLoginSubmitting:
		b.WriteString(m.spinner.View() + " Logging in...")

	case stepLoginDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(tui.SuccessIcon("Logged in successfully."))
		}
	}

	return b.String()
}

// handleEnter advances the flow based on the current step.
func (m loginModel) handleEnter() (tea.Model, tea.Cmd) {
	m.err = nil // clear any previous validation error

	switch m.step {
	case stepLoginEmail:
		email := strings.TrimSpace(m.email.Value())
		if email == "" {
			m.err = fmt.Errorf("email is required")
			return m, nil
		}
		if !strings.Contains(email, "@") {
			m.err = fmt.Errorf("enter a valid email address")
			return m, nil
		}
		m.step = stepLoginPassword
		m.email.Blur()
		m.password.Focus()
		return m, textinput.Blink

	case stepLoginPassword:
		if m.password.Value() == "" {
			m.err = fmt.Errorf("password is required")
			return m, nil
		}
		m.step = stepLoginSubmitting
		return m, tea.Batch(m.spinner.Tick, m.performLogin)

	default:
		return m, nil
	}
}

// performLogin is a tea.Cmd that calls the GRPC Login RPC.
func (m loginModel) performLogin() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return loginResultMsg{err: err}
	}
	defer cleanup()

	resp, err := client.Login(m.ctx, &pb.LoginRequest{
		Email:    m.email.Value(),
		Password: m.password.Value(),
	})
	if err != nil {
		return loginResultMsg{err: fmt.Errorf("login: %w", err)}
	}
	return loginResultMsg{token: resp.Token}
}

// handleResult processes the GRPC response and saves config on success.
func (m loginModel) handleResult(msg loginResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.step = stepLoginDone
		return m, tea.Quit
	}

	m.cfg.Token = msg.token
	if err := saveConfig(m.cfg); err != nil {
		m.err = fmt.Errorf("save config: %w", err)
		m.step = stepLoginDone
		return m, tea.Quit
	}

	m.step = stepLoginDone
	return m, tea.Quit
}
