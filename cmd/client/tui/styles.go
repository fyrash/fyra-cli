// Package tui provides shared styles and utilities for the CLI's terminal UI.
package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Brand colors — neutral base with a single accent.
var (
	ColorText   = lipgloss.Color("#cad3f5")
	ColorMuted  = lipgloss.Color("#6e738d")
	ColorBorder = lipgloss.Color("#363a4f")

	ColorPrimary = lipgloss.Color("#8aadf4")
	ColorSuccess = lipgloss.Color("#a6da95")
	ColorError   = lipgloss.Color("#ed8796")
)

// Reusable lipgloss styles.
var (
	StyleTitle   = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	StyleSuccess = lipgloss.NewStyle().Foreground(ColorSuccess)
	StyleError   = lipgloss.NewStyle().Foreground(ColorError)
	StyleMuted   = lipgloss.NewStyle().Foreground(ColorMuted)
)

// TOSNotice returns a muted notice about agreeing to the Terms of Service.
func TOSNotice() string {
	return StyleMuted.Render("By continuing, you agree to the Terms of Service: https://fyra.sh/tos.html")
}

// SuccessIcon prefixes a check mark to the message using the success color.
func SuccessIcon(msg string) string {
	return StyleSuccess.Render("✔ " + msg)
}

// ErrorIcon prefixes an X mark to the message using the error color.
func ErrorIcon(msg string) string {
	return StyleError.Render("✗ " + msg)
}

// PlanLimitBlock renders a styled amber panel for plan limit errors.
func PlanLimitBlock(msg string) string {
	amber := lipgloss.Color("#f5a623")
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1a1a2e")).
		Background(amber).
		Padding(0, 2).
		Bold(true)
	return "\n" + style.Render("⚠ Plan limit: "+msg) + "\n"
}

// NewEmailInput returns a textinput.Model configured for email entry.
func NewEmailInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "you@example.com"
	ti.Focus()
	ti.CharLimit = 254
	ti.Width = 30
	return ti
}

// NewPasswordInput returns a textinput.Model configured for password entry
// (characters hidden with dots).
func NewPasswordInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "password"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 128
	ti.Width = 30
	return ti
}

// NewSpinner returns a spinner.Model styled with the primary accent color.
func NewSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorPrimary)
	return s
}

// Run launches a Bubble Tea program in inline mode and returns the final
// model. It is a convenience wrapper around tea.NewProgram(...).Run().
func Run(model tea.Model) (tea.Model, error) {
	p := tea.NewProgram(model)
	return p.Run()
}

// NewTableStyles returns the standard lipgloss styles for TUI tables.
func NewTableStyles() table.Styles {
	return table.Styles{
		Header: lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Padding(0, 1),
		Cell:   lipgloss.NewStyle().Foreground(ColorText).Padding(0, 1),
	}
}
