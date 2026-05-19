package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your fyra.sh account",
	RunE:  runLogin,
}

func runLogin(cmd *cobra.Command, _ []string) error {
	if err := ensureConfig(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newLoginModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if lm, ok := final.(loginModel); ok && lm.err != nil {
		return lm.err
	}
	return nil
}
