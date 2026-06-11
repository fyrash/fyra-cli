package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
)

var addonsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List addons attached to an app",
	RunE:  runAddonsList,
}

var addonsInfoCmd = &cobra.Command{
	Use:   "info <addon-id>",
	Short: "Show details and config vars for an addon",
	Args:  cobra.ExactArgs(1),
	RunE:  runAddonsInfo,
}

func init() {
	addonsListCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
	addonsInfoCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
}

func runAddonsList(cmd *cobra.Command, _ []string) error {
	appSlug, domain, err := resolveApp(cmd)
	if err != nil {
		return err
	}

	if absP, err := absCwd(); err == nil && appSlug != "" {
		_ = appindex.Register(appSlug, absP)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	m := newAddonsListModel("", appSlug, domain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	lm, ok := final.(addonsListModel)
	if !ok || lm.err != nil {
		if ok {
			return lm.err
		}
		return fmt.Errorf("unexpected error")
	}
	if lm.empty {
		fmt.Println(tui.StyleMuted.Render("No addons attached. Run 'fyra addons create <addon-id>' to add one."))
	}
	return nil
}

func runAddonsInfo(cmd *cobra.Command, args []string) error {
	addonID := args[0]

	appSlug, domain, err := resolveApp(cmd)
	if err != nil {
		return err
	}

	if absP, err := absCwd(); err == nil && appSlug != "" {
		_ = appindex.Register(appSlug, absP)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	m := newAddonsListModel(addonID, appSlug, domain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	lm, ok := final.(addonsListModel)
	if !ok || lm.err != nil {
		if ok {
			return lm.err
		}
		return fmt.Errorf("unexpected error")
	}
	if lm.empty {
		return fmt.Errorf("addon %s is not attached to this app", addonID)
	}
	return nil
}
