package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var (
	loginEmailFlag         string
	loginPasswordStdinFlag bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your fyra.sh account",
	RunE:  runLogin,
}

func init() {
	loginCmd.Flags().StringVar(&loginEmailFlag, "email", "", "account email (env: FYRA_EMAIL)")
	loginCmd.Flags().BoolVar(&loginPasswordStdinFlag, "password-stdin", false, "read password from stdin (env: FYRA_PASSWORD also triggers non-interactive mode)")
}

func runLogin(cmd *cobra.Command, _ []string) error {
	if err := ensureConfig(); err != nil {
		return fmt.Errorf("init config: %w", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Email precedence: flag > env.
	email := loginEmailFlag
	if email == "" {
		email = os.Getenv("FYRA_EMAIL")
	}

	passwordEnv := os.Getenv("FYRA_PASSWORD")

	// Non-interactive trigger: explicit --password-stdin OR FYRA_PASSWORD set.
	if loginPasswordStdinFlag || passwordEnv != "" {
		password, err := resolvePassword(loginPasswordStdinFlag, passwordEnv)
		if err != nil {
			return err
		}
		if email == "" {
			return fmt.Errorf("non-interactive login requires --email or FYRA_EMAIL")
		}
		return runLoginNonInteractive(cmd.Context(), cfg, email, password, os.Stdout)
	}

	// Interactive TUI path.
	m := newLoginModel(cfg, cmd.Context())
	if loginEmailFlag != "" {
		// Pre-fill the email field so the user only has to type their password.
		m.email.SetValue(loginEmailFlag)
		m.step = stepLoginPassword
		m.email.Blur()
		m.password.Focus()
	}

	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if lm, ok := final.(loginModel); ok && lm.err != nil {
		return lm.err
	}
	return nil
}

// resolvePassword returns the password from stdin (--password-stdin) or from
// the env fallback. Trailing newlines from stdin are trimmed.
func resolvePassword(fromStdin bool, envFallback string) (string, error) {
	if !fromStdin {
		return envFallback, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}
