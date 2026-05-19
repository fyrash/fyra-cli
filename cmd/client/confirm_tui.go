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

type confirmStep int

const (
	stepConfirmCode confirmStep = iota
	stepConfirmSubmitting
	stepConfirmResending
	stepConfirmDone
)

type confirmModel struct {
	step     confirmStep
	input    textinput.Model
	spinner  spinner.Model
	cfg      clientConfig
	ctx      context.Context
	email    string // masked email for display
	err      error
	quitting bool
}

type confirmResultMsg struct{ err error }
type resendResultMsg struct{ err error }

func newConfirmModel(cfg clientConfig, ctx context.Context, email string) confirmModel {
	ti := textinput.New()
	ti.Placeholder = "000000"
	ti.CharLimit = 6
	ti.Width = 10
	ti.Focus()

	return confirmModel{
		step:    stepConfirmCode,
		input:   ti,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		email:   email,
	}
}

func (m confirmModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.step == stepConfirmCode {
				return m.handleSubmit()
			}
		default:
			if msg.String() == "r" && m.step == stepConfirmCode && m.input.Value() == "" {
				return m.handleResend()
			}
		}

	case confirmResultMsg:
		return m.handleResult(msg)
	case resendResultMsg:
		return m.handleResendResult(msg)
	}

	var cmd tea.Cmd
	switch m.step {
	case stepConfirmCode:
		m.input, cmd = m.input.Update(msg)
	case stepConfirmSubmitting, stepConfirmResending:
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

func (m confirmModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepConfirmCode:
		b.WriteString(tui.StyleTitle.Render("Confirm your email"))
		b.WriteString(fmt.Sprintf("\n\nA confirmation code was sent to %s\n\n", tui.StyleMuted.Render(m.email)))
		b.WriteString("Code: ")
		b.WriteString(m.input.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("[r] Resend code    [Ctrl+C] Skip for now"))

	case stepConfirmSubmitting:
		b.WriteString(m.spinner.View() + " Verifying code...")

	case stepConfirmResending:
		b.WriteString(m.spinner.View() + " Sending new code...")

	case stepConfirmDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else {
			b.WriteString(tui.SuccessIcon("Email confirmed! You can now create and deploy apps."))
		}
	}

	return b.String()
}

func (m confirmModel) handleSubmit() (confirmModel, tea.Cmd) {
	code := strings.TrimSpace(m.input.Value())
	if len(code) != 6 {
		m.err = fmt.Errorf("code must be 6 digits")
		return m, nil
	}
	m.step = stepConfirmSubmitting
	return m, tea.Batch(m.spinner.Tick, m.performConfirm(code))
}

func (m confirmModel) performConfirm(code string) tea.Cmd {
	return func() tea.Msg {
		client, cleanup, err := m.cfg.dial()
		if err != nil {
			return confirmResultMsg{err: err}
		}
		defer cleanup()

		ctx := authContext(m.ctx, m.cfg.Token)
		_, err = client.ConfirmEmail(ctx, &pb.ConfirmEmailRequest{Code: code})
		return confirmResultMsg{err: err}
	}
}

func (m confirmModel) handleResult(msg confirmResultMsg) (confirmModel, tea.Cmd) {
	if msg.err != nil {
		m.err = fmt.Errorf("invalid or expired code. Try again or press 'r' to resend.")
		m.step = stepConfirmCode
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	}
	m.err = nil
	m.step = stepConfirmDone
	return m, tea.Quit
}

func (m confirmModel) handleResend() (confirmModel, tea.Cmd) {
	m.step = stepConfirmResending
	m.err = nil
	return m, tea.Batch(m.spinner.Tick, m.performResend)
}

func (m confirmModel) performResend() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return resendResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	_, err = client.ResendConfirmation(ctx, &pb.ResendConfirmationRequest{})
	return resendResultMsg{err: err}
}

func (m confirmModel) handleResendResult(msg resendResultMsg) (confirmModel, tea.Cmd) {
	m.step = stepConfirmCode
	m.input.SetValue("")
	m.input.Focus()
	if msg.err != nil {
		m.err = fmt.Errorf("failed to resend code: %v", msg.err)
	}
	return m, textinput.Blink
}
