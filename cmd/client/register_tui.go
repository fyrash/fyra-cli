package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// registerStep represents the current step in the registration flow.
type registerStep int

const (
	stepRegisterEmail registerStep = iota
	stepRegisterPassword
	stepRegisterConfirm
	stepRegisterSubmitting
	stepRegisterDone
)

// registerModel is the Bubble Tea model for the register command.
type registerModel struct {
	step     registerStep
	email    textinput.Model
	password textinput.Model
	confirm  textinput.Model
	spinner  spinner.Model
	cfg      clientConfig
	ctx      context.Context
	err      error
	quitting bool
}

// registerResultMsg carries the result of the GRPC Register call.
type registerResultMsg struct {
	userID string
	err    error
}

func newRegisterModel(cfg clientConfig, ctx context.Context) registerModel {
	return registerModel{
		step:     stepRegisterEmail,
		email:    tui.NewEmailInput(),
		password: tui.NewPasswordInput(),
		confirm:  tui.NewPasswordInput(),
		spinner:  tui.NewSpinner(),
		cfg:      cfg,
		ctx:      ctx,
	}
}

func (m registerModel) Init() tea.Cmd {
	return textinput.Blink
}

//nolint:revive // cyclomatic is inherent to a state-machine Update.
func (m registerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			if m.step == stepRegisterSubmitting {
				m.step = stepRegisterDone
				m.err = fmt.Errorf("cancelled")
			}
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleEnter()
		}

	case registerResultMsg:
		return m.handleResult(msg)
	}

	var cmd tea.Cmd
	switch m.step {
	case stepRegisterEmail:
		m.email, cmd = m.email.Update(msg)
	case stepRegisterPassword:
		m.password, cmd = m.password.Update(msg)
	case stepRegisterConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	case stepRegisterSubmitting:
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

func (m registerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepRegisterEmail:
		b.WriteString(tui.StyleTitle.Render("Create your fyra.sh account"))
		b.WriteString("\n\n" + tui.TOSNotice())
		b.WriteString("\n\nEmail:\n")
		b.WriteString(m.email.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepRegisterPassword:
		b.WriteString(tui.StyleTitle.Render("Create your fyra.sh account"))
		b.WriteString("\n\n")
		b.WriteString(tui.StyleMuted.Render("Email: " + m.email.Value()))
		b.WriteString("\n\nPassword:\n")
		b.WriteString(m.password.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepRegisterConfirm:
		b.WriteString(tui.StyleTitle.Render("Create your fyra.sh account"))
		b.WriteString("\n\n")
		b.WriteString(tui.StyleMuted.Render("Email: " + m.email.Value()))
		b.WriteString("\n\nConfirm password:\n")
		b.WriteString(m.confirm.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to register · esc to cancel"))

	case stepRegisterSubmitting:
		b.WriteString(m.spinner.View() + " Creating account...")

	case stepRegisterDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(tui.SuccessIcon("Account created."))
			b.WriteString("\n" + tui.StyleMuted.Render("Run 'fyra login' to get started."))
		}
	}

	return b.String()
}

// handleEnter advances the flow based on the current step.
func (m registerModel) handleEnter() (tea.Model, tea.Cmd) {
	m.err = nil

	switch m.step {
	case stepRegisterEmail:
		email := strings.TrimSpace(m.email.Value())
		if email == "" {
			m.err = fmt.Errorf("email is required")
			return m, nil
		}
		if !strings.Contains(email, "@") {
			m.err = fmt.Errorf("enter a valid email address")
			return m, nil
		}
		m.step = stepRegisterPassword
		m.email.Blur()
		m.password.Focus()
		return m, textinput.Blink

	case stepRegisterPassword:
		if m.password.Value() == "" {
			m.err = fmt.Errorf("password is required")
			return m, nil
		}
		if len(m.password.Value()) < 8 {
			m.err = fmt.Errorf("password must be at least 8 characters")
			return m, nil
		}
		m.step = stepRegisterConfirm
		m.password.Blur()
		m.confirm.Focus()
		return m, textinput.Blink

	case stepRegisterConfirm:
		if err := validatePasswordMatch(m.password.Value(), m.confirm.Value()); err != nil {
			m.err = err
			return m, nil
		}
		m.step = stepRegisterSubmitting
		return m, tea.Batch(m.spinner.Tick, m.performRegister)

	default:
		return m, nil
	}
}

// performRegister is a tea.Cmd that calls the GRPC Register RPC.
func (m registerModel) performRegister() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return registerResultMsg{err: err}
	}
	defer cleanup()

	resp, err := client.Register(m.ctx, &pb.RegisterRequest{
		Email:    m.email.Value(),
		Password: m.password.Value(),
	})
	if err != nil {
		return registerResultMsg{err: fmt.Errorf("register: %w", err)}
	}
	return registerResultMsg{userID: resp.UserId}
}

// handleResult processes the GRPC response.
func (m registerModel) handleResult(msg registerResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		m.step = stepRegisterDone
		return m, tea.Quit
	}

	m.step = stepRegisterDone
	return m, tea.Quit
}

// validatePasswordMatch returns an error if password and confirm differ.
func validatePasswordMatch(password, confirm string) error {
	if password != confirm {
		return errors.New("passwords do not match")
	}
	return nil
}
