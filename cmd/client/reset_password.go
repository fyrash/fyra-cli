package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var resetPasswordCmd = &cobra.Command{
	Use:   "reset-password",
	Short: "Reset your password via email verification",
	RunE:  runResetPassword,
}

func runResetPassword(cmd *cobra.Command, _ []string) error {
	if err := ensureConfig(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newResetPasswordModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	rm, ok := final.(resetPasswordModel)
	if !ok || rm.err != nil {
		if ok {
			return rm.err
		}
		return fmt.Errorf("unexpected error")
	}

	fmt.Println(tui.SuccessIcon("Password reset! Run '" + binaryName + " auth login' to sign in."))
	return nil
}
