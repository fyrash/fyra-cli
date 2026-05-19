package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the currently logged-in account email",
	RunE:  runWhoAmI,
}

func runWhoAmI(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newWhoamiModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	wm, ok := final.(whoamiModel)
	if !ok || wm.err != nil {
		if ok {
			return wm.err
		}
		return fmt.Errorf("unexpected error")
	}

	fmt.Println(wm.email)
	if !wm.confirmed {
		fmt.Println(tui.StyleMuted.Render("(unconfirmed — run '" + binaryName + " confirm')"))
	}
	return nil
}
