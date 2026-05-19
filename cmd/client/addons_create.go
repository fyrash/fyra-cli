package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var addonsCreateCmd = &cobra.Command{
	Use:   "create <addon-id>",
	Short: "Attach an addon to an app",
	Args:  cobra.ExactArgs(1),
	RunE:  runAddonsCreate,
}

func init() {
	addonsCreateCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
	addonsCreateCmd.Flags().String("plan", "", "addon plan (default: first available)")
}

func runAddonsCreate(cmd *cobra.Command, args []string) error {
	addonID := args[0]

	appSlug, domain, err := resolveApp(cmd)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	plan, _ := cmd.Flags().GetString("plan")
	m := newAddonsCreateModel(addonID, appSlug, domain, plan, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	cm, ok := final.(addonsCreateModel)
	if !ok || cm.err != nil {
		if ok {
			return cm.err
		}
		return fmt.Errorf("unexpected error")
	}

	fmt.Printf("Addon %s attached to %s.%s\n\n", addonID, appSlug, domain)
	fmt.Println(tui.StyleTitle.Render("Config vars"))
	for k, v := range cm.config {
		fmt.Printf("  %s=%s\n", k, v)
	}
	if cm.message != "" {
		fmt.Println()
		fmt.Println(tui.StyleTitle.Render("Setup"))
		fmt.Println(cm.message)
	}
	return nil
}

// resolveApp returns the app slug and domain from either --app flag or .deploy.yaml.
func resolveApp(cmd *cobra.Command) (slug, domain string, err error) {
	appFlag, _ := cmd.Flags().GetString("app")
	if appFlag != "" {
		// --app flag provided: read domain from .deploy.yaml if present, else empty (server default)
		af, readErr := readAppFile()
		if readErr == nil && af.Domain != "" {
			return appFlag, af.Domain, nil
		}
		return appFlag, "", nil
	}
	af, err := readAppFile()
	if err != nil {
		return "", "", err
	}
	return af.Slug, af.Domain, nil
}
