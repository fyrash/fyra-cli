package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var confirmCmd = &cobra.Command{
	Use:   "confirm",
	Short: "Confirm your email address",
	RunE:  runConfirm,
}

func runConfirm(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	client, cleanup, err := cfg.dial()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := authContext(cmd.Context(), cfg.Token)

	resp, err := client.WhoAmI(ctx, &pb.WhoAmIRequest{})
	if err != nil {
		return fmt.Errorf("whoami: %w", err)
	}
	if resp.Confirmed {
		fmt.Println("Your email is already confirmed.")
		return nil
	}

	if _, err := client.ResendConfirmation(ctx, &pb.ResendConfirmationRequest{}); err != nil {
		return fmt.Errorf("resend confirmation: %w", err)
	}

	masked := maskEmail(resp.Email)
	m := newConfirmModel(cfg, cmd.Context(), masked)
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if cm, ok := final.(confirmModel); ok {
		if cm.err != nil {
			return cm.err
		}
		fmt.Println(tui.SuccessIcon("Email confirmed! You can now create and deploy apps."))
	}
	return nil
}

// maskEmail masks the local part of an email for display.
// "david@example.com" → "d***@example.com"
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "***"
	}
	local := parts[0]
	if len(local) == 0 {
		return "***@" + parts[1]
	}
	return string(local[0]) + "***@" + parts[1]
}
