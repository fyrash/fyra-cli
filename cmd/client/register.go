package main

import (
	"fmt"

	"github.com/spf13/cobra"
	pb "github.com/fyrash/fyra-cli/proto/gen"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Create a new fyra.sh account",
	RunE:  runRegister,
}

func runRegister(cmd *cobra.Command, _ []string) error {
	if err := ensureConfig(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newRegisterModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	rm, ok := final.(registerModel)
	if !ok || rm.err != nil {
		if ok {
			return rm.err
		}
		return fmt.Errorf("unexpected error")
	}

	// Registration succeeded — auto-login to get a token, then show confirmation.
	client, cleanup, err := cfg.dial()
	if err != nil {
		fmt.Println(tui.SuccessIcon("Account created."))
		fmt.Println(tui.StyleMuted.Render("Run 'fyra login' then 'fyra confirm' to verify your email."))
		return nil
	}
	defer cleanup()

	loginResp, err := client.Login(cmd.Context(), &pb.LoginRequest{
		Email:    rm.email.Value(),
		Password: rm.password.Value(),
	})
	if err != nil {
		fmt.Println(tui.SuccessIcon("Account created."))
		fmt.Println(tui.StyleMuted.Render("Run 'fyra login' then 'fyra confirm' to verify your email."))
		return nil
	}

	cfg.Token = loginResp.Token
	if err := saveConfig(cfg); err != nil {
		fmt.Println(tui.SuccessIcon("Account created."))
		fmt.Println(tui.StyleMuted.Render("Run 'fyra login' then 'fyra confirm' to verify your email."))
		return nil
	}

	masked := maskEmail(rm.email.Value())
	cm := newConfirmModel(cfg, cmd.Context(), masked)
	cfinal, err := tui.Run(cm)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if cfm, ok := cfinal.(confirmModel); ok && cfm.err != nil {
		return cfm.err
	}
	return nil
}
