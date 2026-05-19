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

type resetPasswordStep int

const (
	stepResetEmail resetPasswordStep = iota
	stepResetSending
	stepResetCode
	stepResetResending
	stepResetPassword
	stepResetConfirm
	stepResetSubmitting
	stepResetDone
)

type resetPasswordModel struct {
	step     resetPasswordStep
	email    textinput.Model
	code     textinput.Model
	password textinput.Model
	confirm  textinput.Model
	spinner  spinner.Model
	cfg      clientConfig
	ctx      context.Context
	err      error
	quitting bool
}

// resetSendResultMsg carries the result of a RequestPasswordReset RPC.
// The resend flag routes the response to the correct handler.
type resetSendResultMsg struct {
	err    error
	resend bool
}

type resetConfirmResultMsg struct{ err error }

func newResetPasswordModel(cfg clientConfig, ctx context.Context) resetPasswordModel {
	return resetPasswordModel{
		step:     stepResetEmail,
		email:    tui.NewEmailInput(),
		code:     newResetCodeInput(),
		password: tui.NewPasswordInput(),
		confirm:  tui.NewPasswordInput(),
		spinner:  tui.NewSpinner(),
		cfg:      cfg,
		ctx:      ctx,
	}
}

func newResetCodeInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "000000"
	ti.CharLimit = 6
	ti.Width = 10
	ti.Focus()
	return ti
}

func (m resetPasswordModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m resetPasswordModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleEnter()
		default:
			if msg.String() == "r" && m.step == stepResetCode && m.code.Value() == "" {
				return m.handleResend()
			}
		}

	case resetSendResultMsg:
		if msg.resend {
			return m.handleResendResult(msg)
		}
		return m.handleSendResult(msg)
	case resetConfirmResultMsg:
		return m.handleConfirmResult(msg)
	}

	var cmd tea.Cmd
	switch m.step {
	case stepResetEmail:
		m.email, cmd = m.email.Update(msg)
	case stepResetCode:
		m.code, cmd = m.code.Update(msg)
	case stepResetPassword:
		m.password, cmd = m.password.Update(msg)
	case stepResetConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	case stepResetSending, stepResetResending, stepResetSubmitting:
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

func (m resetPasswordModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepResetEmail:
		b.WriteString(tui.StyleTitle.Render("Reset your password"))
		b.WriteString("\n\nEmail:\n")
		b.WriteString(m.email.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepResetSending:
		b.WriteString(m.spinner.View() + " Sending reset code...")

	case stepResetCode:
		b.WriteString(tui.StyleTitle.Render("Reset your password"))
		b.WriteString("\n\n")
		b.WriteString(tui.StyleMuted.Render("If that email is registered, a reset code has been sent."))
		b.WriteString(fmt.Sprintf("\nCode sent to %s\n\n", tui.StyleMuted.Render(maskEmail(m.email.Value()))))
		b.WriteString("Code: ")
		b.WriteString(m.code.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("[r] Resend code    [Ctrl+C] Cancel"))

	case stepResetResending:
		b.WriteString(m.spinner.View() + " Sending new code...")

	case stepResetPassword:
		b.WriteString(tui.StyleTitle.Render("Reset your password"))
		b.WriteString("\n\nNew password:\n")
		b.WriteString(m.password.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepResetConfirm:
		b.WriteString(tui.StyleTitle.Render("Reset your password"))
		b.WriteString("\n\nConfirm new password:\n")
		b.WriteString(m.confirm.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to reset · esc to cancel"))

	case stepResetSubmitting:
		b.WriteString(m.spinner.View() + " Resetting password...")

	case stepResetDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(tui.SuccessIcon("Password reset! Run '" + binaryName + " auth login' to sign in."))
		}
	}

	return b.String()
}

func (m resetPasswordModel) handleEnter() (tea.Model, tea.Cmd) {
	m.err = nil

	switch m.step {
	case stepResetEmail:
		email := strings.TrimSpace(m.email.Value())
		if email == "" {
			m.err = fmt.Errorf("email is required")
			return m, nil
		}
		if !strings.Contains(email, "@") {
			m.err = fmt.Errorf("enter a valid email address")
			return m, nil
		}
		m.step = stepResetSending
		return m, tea.Batch(m.spinner.Tick, m.performSend(email))

	case stepResetCode:
		code := strings.TrimSpace(m.code.Value())
		if len(code) != 6 {
			m.err = fmt.Errorf("code must be 6 digits")
			return m, nil
		}
		m.step = stepResetPassword
		m.code.Blur()
		m.password.Focus()
		return m, textinput.Blink

	case stepResetPassword:
		if m.password.Value() == "" {
			m.err = fmt.Errorf("password is required")
			return m, nil
		}
		if len(m.password.Value()) < 8 {
			m.err = fmt.Errorf("password must be at least 8 characters")
			return m, nil
		}
		m.step = stepResetConfirm
		m.password.Blur()
		m.confirm.Focus()
		return m, textinput.Blink

	case stepResetConfirm:
		if m.password.Value() != m.confirm.Value() {
			m.err = fmt.Errorf("passwords do not match")
			return m, nil
		}
		m.step = stepResetSubmitting
		return m, tea.Batch(m.spinner.Tick, m.performConfirm())

	default:
		return m, nil
	}
}

func (m resetPasswordModel) performSend(email string) tea.Cmd {
	return func() tea.Msg {
		client, cleanup, err := m.cfg.dial()
		if err != nil {
			return resetSendResultMsg{err: err}
		}
		defer cleanup()

		_, err = client.RequestPasswordReset(m.ctx, &pb.RequestPasswordResetRequest{Email: email})
		return resetSendResultMsg{err: err}
	}
}

func (m resetPasswordModel) performResend(email string) tea.Cmd {
	return func() tea.Msg {
		client, cleanup, err := m.cfg.dial()
		if err != nil {
			return resetSendResultMsg{err: err, resend: true}
		}
		defer cleanup()

		_, err = client.RequestPasswordReset(m.ctx, &pb.RequestPasswordResetRequest{Email: email})
		return resetSendResultMsg{err: err, resend: true}
	}
}

func (m resetPasswordModel) performConfirm() tea.Cmd {
	email := m.email.Value()
	code := strings.TrimSpace(m.code.Value())
	newPassword := m.password.Value()

	return func() tea.Msg {
		client, cleanup, err := m.cfg.dial()
		if err != nil {
			return resetConfirmResultMsg{err: err}
		}
		defer cleanup()

		_, err = client.ConfirmPasswordReset(m.ctx, &pb.ConfirmPasswordResetRequest{
			Code:        code,
			NewPassword: newPassword,
			Email:       email,
		})
		return resetConfirmResultMsg{err: err}
	}
}

func (m resetPasswordModel) handleSendResult(msg resetSendResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = fmt.Errorf("failed to send reset code: %v", msg.err)
		m.step = stepResetEmail
		return m, textinput.Blink
	}
	m.step = stepResetCode
	m.code.Focus()
	return m, textinput.Blink
}

func (m resetPasswordModel) handleConfirmResult(msg resetConfirmResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = fmt.Errorf("failed to reset password. Your code may have expired.")
		m.step = stepResetDone
		return m, tea.Quit
	}
	m.step = stepResetDone
	return m, tea.Quit
}

func (m resetPasswordModel) handleResend() (tea.Model, tea.Cmd) {
	m.step = stepResetResending
	m.err = nil
	return m, tea.Batch(m.spinner.Tick, m.performResend(m.email.Value()))
}

func (m resetPasswordModel) handleResendResult(msg resetSendResultMsg) (tea.Model, tea.Cmd) {
	m.step = stepResetCode
	m.code.SetValue("")
	m.code.Focus()
	if msg.err != nil {
		m.err = fmt.Errorf("failed to resend code: %v", msg.err)
	}
	return m, textinput.Blink
}
