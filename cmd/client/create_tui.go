package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// createStep represents the current step in the create flow.
type createStep int

const (
	stepCreateFetchDomains createStep = iota
	stepCreateChooseDomain
	stepCreateEnterAppname
	stepCreateCreating
	stepCreateDone
)

// createModel is the Bubble Tea model for the create command.
type createModel struct {
	step           createStep
	spinner        spinner.Model
	cfg            clientConfig
	ctx            context.Context
	appname        string          // from --appname flag (pre-fill)
	freeDomains    []string        // fetched from server
	selectedDomain string          // chosen free domain zone
	domainInput    textinput.Model // for domain selection (number)
	appnameInput   textinput.Model // for appname entry
	slug           string
	domain         string // stored domain for the app
	createdAt      string
	err            error
	planErr        error
	quitting       bool
}

// freeDomainsResultMsg carries the result of the GetFreeDomains RPC.
type freeDomainsResultMsg struct {
	domains []string
	err     error
}

// createResultMsg carries the result of the GRPC CreateApp call.
type createResultMsg struct {
	slug         string
	domain       string
	createdAt    string
	err          error
}

func newCreateModel(appname string, cfg clientConfig, ctx context.Context) createModel {
	di := textinput.New()
	di.Prompt = "> "
	di.CharLimit = 253
	di.Width = 40

	ai := textinput.New()
	ai.Prompt = "> "
	ai.CharLimit = 63
	ai.Width = 40

	return createModel{
		step:         stepCreateFetchDomains,
		spinner:      tui.NewSpinner(),
		cfg:          cfg,
		ctx:          ctx,
		appname:      appname,
		domainInput:  di,
		appnameInput: ai,
	}
}

func (m createModel) Init() tea.Cmd {
	if m.cfg.Token == "" {
		return func() tea.Msg {
			return createResultMsg{err: fmt.Errorf("not logged in: run '%s login' first", binaryName)}
		}
	}
	return tea.Batch(m.spinner.Tick, m.fetchFreeDomains)
}

//nolint:revive // cyclomatic is inherent to a state-machine Update.
func (m createModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			m.step = stepCreateDone
			m.err = fmt.Errorf("cancelled")
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleEnter()
		}

	case freeDomainsResultMsg:
		return m.handleFreeDomains(msg)

	case createResultMsg:
		return m.handleCreateResult(msg)
	}

	var cmd tea.Cmd
	switch m.step {
	case stepCreateFetchDomains, stepCreateCreating:
		m.spinner, cmd = m.spinner.Update(msg)
	case stepCreateChooseDomain:
		m.domainInput, cmd = m.domainInput.Update(msg)
	case stepCreateEnterAppname:
		m.appnameInput, cmd = m.appnameInput.Update(msg)
	}
	return m, cmd
}

func (m createModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.step {
	case stepCreateFetchDomains:
		b.WriteString(m.spinner.View() + " Loading domains...")

	case stepCreateChooseDomain:
		b.WriteString(tui.StyleTitle.Render("Choose a subdomain"))
		b.WriteString("\n\n")
		b.WriteString(tui.TOSNotice() + "\n\n")
		for i, d := range m.freeDomains {
			fmt.Fprintf(&b, "  %d. *.%s\n", i+1, d)
		}
		b.WriteString("\n")
		b.WriteString(m.domainInput.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to continue · esc to cancel"))

	case stepCreateEnterAppname:
		b.WriteString(tui.StyleTitle.Render("Choose an appname"))
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "%s.%s\n", tui.StyleMuted.Render("Domain"), m.selectedDomain)
		b.WriteString("\n")
		b.WriteString(m.appnameInput.View())
		if m.err != nil {
			b.WriteString("\n" + tui.ErrorIcon(m.err.Error()))
		}
		b.WriteString("\n\n" + tui.StyleMuted.Render("enter to create · esc to cancel"))

	case stepCreateCreating:
		label := "Creating app..."
		if m.selectedDomain != "" {
			label = fmt.Sprintf("Creating app %s.%s...", m.appnameInput.Value(), m.selectedDomain)
		}
		b.WriteString(m.spinner.View() + " " + label)

	case stepCreateDone:
		// Output printed by create.go after tui.Run() returns.
	}

	return b.String()
}

// handleEnter advances the flow based on the current step.
func (m createModel) handleEnter() (tea.Model, tea.Cmd) {
	m.err = nil

	switch m.step {
	case stepCreateChooseDomain:
		return m.handleDomainChoice()

	case stepCreateEnterAppname:
		return m.handleAppnameSubmit()

	default:
		return m, nil
	}
}

// handleDomainChoice processes the user's domain selection input.
func (m createModel) handleDomainChoice() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.domainInput.Value())
	if input == "" {
		m.err = fmt.Errorf("please enter a number")
		return m, nil
	}

	idx, err := strconv.Atoi(input)
	if err != nil {
		m.err = fmt.Errorf("please enter a number 1-%d", len(m.freeDomains))
		return m, nil
	}
	if idx < 1 || idx > len(m.freeDomains) {
		m.err = fmt.Errorf("enter a number 1-%d", len(m.freeDomains))
		return m, nil
	}
	m.selectedDomain = m.freeDomains[idx-1]

	// If --appname was provided, skip appname input and go straight to creating.
	if m.appname != "" {
		m.appnameInput.SetValue(m.appname)
		m.step = stepCreateCreating
		return m, tea.Batch(m.spinner.Tick, m.performCreate)
	}

	m.domainInput.Blur()
	m.appnameInput.Focus()
	m.step = stepCreateEnterAppname
	return m, textinput.Blink
}

// handleAppnameSubmit validates and submits the appname.
func (m createModel) handleAppnameSubmit() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.appnameInput.Value())
	if name == "" {
		m.err = fmt.Errorf("appname is required")
		return m, nil
	}
	m.step = stepCreateCreating
	return m, tea.Batch(m.spinner.Tick, m.performCreate)
}

// handleFreeDomains processes the result of the GetFreeDomains RPC.
func (m createModel) handleFreeDomains(msg freeDomainsResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.step = stepCreateDone
		m.err = msg.err
		return m, tea.Quit
	}
	m.freeDomains = msg.domains
	m.domainInput.Focus()
	m.step = stepCreateChooseDomain
	return m, textinput.Blink
}

// handleCreateResult processes the CreateApp RPC response.
// On slug-taken errors for subdomain apps, it goes back to appname input for retry.
func (m createModel) handleCreateResult(msg createResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if isEmailNotConfirmed(msg.err) {
			m.step = stepCreateDone
			m.err = fmt.Errorf("please confirm your email first. Run '%s confirm' to resend the code.", binaryName)
			return m, tea.Quit
		}
		code := status.Code(msg.err)
		// If slug taken on a subdomain path, allow retry.
		if code == codes.AlreadyExists && m.selectedDomain != "" {
			fqdn := m.appnameInput.Value() + "." + m.selectedDomain
			m.err = fmt.Errorf("%s is taken. Please try again.", fqdn)
			m.appnameInput.SetValue("")
			m.step = stepCreateEnterAppname
			return m, textinput.Blink
		}
		if code == codes.ResourceExhausted || code == codes.PermissionDenied {
			m.planErr = msg.err
			m.step = stepCreateDone
			return m, tea.Quit
		}
		m.step = stepCreateDone
		m.err = msg.err
		return m, tea.Quit
	}

	m.slug = msg.slug
	m.domain = msg.domain
	m.createdAt = msg.createdAt
	m.step = stepCreateDone
	return m, tea.Quit
}

// fetchFreeDomains calls the GetFreeDomains RPC.
func (m createModel) fetchFreeDomains() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return freeDomainsResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)
	resp, err := client.GetFreeDomains(ctx, &pb.GetFreeDomainsRequest{})
	if err != nil {
		return freeDomainsResultMsg{err: fmt.Errorf("fetch domains: %w", err)}
	}
	return freeDomainsResultMsg{domains: resp.Domains}
}

// performCreate calls the GRPC CreateApp RPC.
func (m createModel) performCreate() tea.Msg {
	client, cleanup, err := m.cfg.dial()
	if err != nil {
		return createResultMsg{err: err}
	}
	defer cleanup()

	ctx := authContext(m.ctx, m.cfg.Token)

	var req pb.CreateAppRequest
	req.SlugName = strings.TrimSpace(m.appnameInput.Value())
	req.Domain = m.selectedDomain

	resp, err := client.CreateApp(ctx, &req)
	if err != nil {
		return createResultMsg{err: fmt.Errorf("create app: %w", err)}
	}
	return createResultMsg{
		slug:         resp.SlugName,
		domain:       resp.Domain,
		createdAt:    resp.CreatedAt,
	}
}
