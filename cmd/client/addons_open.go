package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
)

var addonsOpenCmd = &cobra.Command{
	Use:   "open <addon-id>",
	Short: "Open an addon's dashboard in your browser",
	Args:  cobra.ExactArgs(1),
	RunE:  runAddonsOpen,
}

func init() {
	addonsOpenCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
	addonsOpenCmd.Flags().Bool("print", false, "print the URL only; do not open a browser")
}

func runAddonsOpen(cmd *cobra.Command, args []string) error {
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

	m := newAddonsOpenModel(addonID, appSlug, domain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	om, ok := final.(addonsOpenModel)
	if !ok || om.err != nil {
		if ok {
			return om.err
		}
		return fmt.Errorf("unexpected error")
	}

	fmt.Println(om.redirectURL)

	printOnly, _ := cmd.Flags().GetBool("print")
	if printOnly {
		return nil
	}

	if err := openBrowser(om.redirectURL); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open browser automatically (%v)\n", err)
		fmt.Fprintln(cmd.ErrOrStderr(), "Open the URL above manually.")
	}
	return nil
}
