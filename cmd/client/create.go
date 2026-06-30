package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

// appFile is the structure of the .deploy.yaml file written to the project directory.
type appFile struct {
	Slug         string                 `yaml:"slug"`
	Domain       string                 `yaml:"domain,omitempty"`
	Server       string                 `yaml:"server"`
	CreatedAt    string                 `yaml:"created_at"`
	CustomDomain string                 `yaml:"custom_domain,omitempty"`
	Config       map[string]interface{} `yaml:"config,omitempty"`
}

// writeAppFile writes af to .deploy.yaml in the current directory.
func writeAppFile(af appFile) error {
	data, err := yaml.Marshal(af)
	if err != nil {
		return fmt.Errorf("marshal .deploy.yaml: %w", err)
	}
	return os.WriteFile(".deploy.yaml", data, 0644)
}

// readAppFile reads .deploy.yaml from the current directory.
func readAppFile() (appFile, error) {
	data, err := os.ReadFile(".deploy.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			return appFile{}, fmt.Errorf("no .deploy.yaml found: run '%s create' first", binaryName)
		}
		return appFile{}, fmt.Errorf("read .deploy.yaml: %w", err)
	}
	var af appFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return appFile{}, fmt.Errorf("parse .deploy.yaml: %w", err)
	}
	return af, nil
}

// removeAppFile removes .deploy.yaml from the current directory, if it exists.
func removeAppFile() error {
	if _, err := os.Stat(".deploy.yaml"); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(".deploy.yaml")
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new app in the current directory",
	RunE:  runCreate,
}

func init() {
	createCmd.Flags().String("appname", "", "app slug name (default: auto-generated)")
	createCmd.Flags().String("domain", "", "free domain zone for the app (non-interactive mode; requires --appname)")
}

func runCreate(cmd *cobra.Command, _ []string) error {
	if _, err := os.Stat(".deploy.yaml"); err == nil {
		return fmt.Errorf("app already initialised in this directory, see .deploy.yaml")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	appname, _ := cmd.Flags().GetString("appname")
	domainFlag, _ := cmd.Flags().GetString("domain")

	// Non-interactive mode: --domain set skips the TUI entirely.
	if domainFlag != "" {
		if appname == "" {
			return fmt.Errorf("non-interactive mode requires --appname when --domain is set")
		}
		if cfg.Token == "" {
			return fmt.Errorf("not logged in: run '%s login' first", binaryName)
		}
		return runCreateNonInteractive(cmd.Context(), cfg, appname, domainFlag, os.Stdout)
	}

	m := newCreateModel(appname, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	cm, ok := final.(createModel)
	if !ok {
		return fmt.Errorf("unexpected error")
	}
	if cm.planErr != nil {
		fmt.Print(tui.PlanLimitBlock(cm.planErr.Error()))
		return nil
	}
	if cm.err != nil {
		return cm.err
	}

	if err := writeAppFile(appFile{
		Slug:      cm.slug,
		Domain:    cm.domain,
		Server:    cfg.ServerAddress,
		CreatedAt: cm.createdAt,
	}); err != nil {
		return err
	}

	fmt.Printf("Created app: %s.%s\n", cm.slug, cm.domain)
	fmt.Printf("Run '%s push' to deploy this directory.\n", binaryName)
	return nil
}

// absCwd returns the absolute path of the current working directory.
func absCwd() (string, error) {
	return os.Getwd()
}

// createFn is the gRPC call seam for the non-interactive create path. It takes
// the already-authenticated context and request and returns the server
// response. Production callers pass client.CreateApp; tests pass a stub.
type createFn func(ctx context.Context, req *pb.CreateAppRequest) (*pb.CreateAppResponse, error)

// runCreateNonInteractive creates an app without launching the TUI. It is the
// CI-friendly path invoked when --domain is set. It dials the server using the
// same plumbing as the TUI path, then delegates to createNonInteractive.
func runCreateNonInteractive(ctx context.Context, cfg clientConfig, appname, domain string, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()
	create := func(ctx context.Context, req *pb.CreateAppRequest) (*pb.CreateAppResponse, error) {
		return client.CreateApp(ctx, req)
	}
	return createNonInteractive(ctx, cfg, appname, domain, out, create)
}

// createNonInteractive is the testable core of the non-interactive create path.
// The createFn parameter is the seam: production passes client.CreateApp,
// tests pass a stub — no real network needed.
func createNonInteractive(
	ctx context.Context,
	cfg clientConfig,
	appname, domain string,
	out io.Writer,
	create createFn,
) error {
	authCtx := authContext(ctx, cfg.Token)
	resp, err := create(authCtx, &pb.CreateAppRequest{
		SlugName: strings.TrimSpace(appname),
		Domain:   domain,
	})
	if err != nil {
		return fmt.Errorf("create app: %w", err)
	}

	if err := writeAppFile(appFile{
		Slug:      resp.SlugName,
		Domain:    resp.Domain,
		Server:    cfg.ServerAddress,
		CreatedAt: resp.CreatedAt,
	}); err != nil {
		return err
	}

	fmt.Fprintf(out, "Created app: %s.%s\n", resp.SlugName, resp.Domain)
	fmt.Fprintf(out, "Run '%s push' to deploy this directory.\n", binaryName)
	return nil
}
