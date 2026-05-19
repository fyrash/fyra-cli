package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// deleteStep represents the current step in the delete flow.
type deleteStep int

const (
	stepDeleteConfirm deleteStep = iota
	stepDeleting
	stepDeleteDone
)

// deleteModel is the Bubble Tea model for the delete command.
type deleteModel struct {
	step     deleteStep
	spinner  spinner.Model
	cfg      clientConfig
	ctx      context.Context
	slug     string
	domain   string
	confirmed bool
	err      error
}

// deleteResultMsg carries the result of the gRPC DeleteApp call.
type deleteResultMsg struct {
	slug string
	err  error
}

func newDeleteModel(slug, domain string, cfg clientConfig, ctx context.Context) deleteModel {
	return deleteModel{
		step:    stepDeleteConfirm,
		spinner: tui.NewSpinner(),
		cfg:     cfg,
		ctx:     ctx,
		slug:    slug,
		domain:  domain,
	}
}

func (m deleteModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return deleteResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return m.spinner.Tick
}

func (m deleteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case stepDeleteConfirm:
			switch msg.String() {
			case "y", "Y":
				m.confirmed = true
				m.step = stepDeleting
				return m, tea.Batch(m.spinner.Tick, m.performDelete)
			case "n", "N":
				m.step = stepDeleteDone
				return m, tea.Quit
			}
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
		case stepDeleting:
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
		case stepDeleteDone:
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
		}

	case deleteResultMsg:
		m.step = stepDeleteDone
		if msg.err != nil {
			m.err = msg.err
		}
		return m, tea.Quit
	}

	var cmd tea.Cmd
	if m.step == stepDeleting {
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

func (m deleteModel) View() string {
	var b strings.Builder

	switch m.step {
	case stepDeleteConfirm:
		b.WriteString(fmt.Sprintf("Delete %s? [y/N] ", tui.StyleTitle.Render(m.slug)))

	case stepDeleting:
		b.WriteString(m.spinner.View() + " Deleting " + m.slug + "...")

	case stepDeleteDone:
		if m.err != nil {
			b.WriteString(tui.ErrorIcon(m.err.Error()))
		} else if m.confirmed {
			b.WriteString(tui.SuccessIcon("App " + m.slug + " deleted."))
		}
	}

	return b.String()
}

// performDelete calls the gRPC DeleteApp endpoint.
func (m deleteModel) performDelete() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return deleteResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	_, err = client.DeleteApp(ctx, &pb.DeleteAppRequest{
		SlugName: m.slug,
		Domain:   m.domain,
	})
	if err != nil {
		return deleteResultMsg{err: friendlyDeleteError(err)}
	}
	return deleteResultMsg{slug: m.slug}
}

func friendlyDeleteError(err error) error {
	code := status.Code(err)
	switch code {
	case codes.NotFound:
		return fmt.Errorf("app not found — check the slug with '%s list'", binaryName)
	case codes.PermissionDenied:
		return fmt.Errorf("you don't have permission to delete this app")
	case codes.Unauthenticated:
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	default:
		return fmt.Errorf("delete failed: %w", err)
	}
}
