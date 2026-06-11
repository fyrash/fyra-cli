package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
)

var addonsDestroyCmd = &cobra.Command{
	Use:   "destroy <addon-id>",
	Short: "Detach and deprovision an addon from an app",
	Args:  cobra.ExactArgs(1),
	RunE:  runAddonsDestroy,
}

func init() {
	addonsDestroyCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
}

func runAddonsDestroy(cmd *cobra.Command, args []string) error {
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

	fmt.Printf("This will deprovision the %s addon from %s.%s.\n", addonID, appSlug, domain)
	fmt.Print("Type the addon name to confirm: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	if input != addonID {
		return fmt.Errorf("cancelled")
	}

	m := newAddonsDestroyModel(addonID, appSlug, domain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	dm, ok := final.(addonsDestroyModel)
	if !ok || dm.err != nil {
		if ok {
			return dm.err
		}
		return fmt.Errorf("unexpected error")
	}

	fmt.Printf("Addon %s deprovisioned from %s.%s\n", addonID, appSlug, domain)
	return nil
}
