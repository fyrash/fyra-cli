package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and clear your local session",
	RunE:  runLogout,
}

func runLogout(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newLogoutModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if lm, ok := final.(logoutModel); ok && lm.err != nil {
		return lm.err
	}
	return nil
}
