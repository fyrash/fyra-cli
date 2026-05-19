package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

// appFile is the structure of the .deploy.yaml file written to the project directory.
type appFile struct {
	Slug         string `yaml:"slug"`
	Domain       string `yaml:"domain,omitempty"`
	Server       string `yaml:"server"`
	CreatedAt    string `yaml:"created_at"`
	CustomDomain string `yaml:"custom_domain,omitempty"`
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
