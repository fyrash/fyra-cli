package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the currently logged-in account email",
	RunE:  runWhoAmI,
}

func init() {
	whoamiCmd.Flags().String("output", "", "output format (text); default is the interactive TUI")
}

func runWhoAmI(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")

	// Non-interactive mode: --output text skips the TUI entirely.
	if output != "" {
		if output != "text" {
			return fmt.Errorf("invalid --output %q: only \"text\" is supported", output)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not logged in: run '%s login' first", binaryName)
		}
		return runWhoamiText(cmd.Context(), cfg, cmd.OutOrStdout())
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

// whoamiFn is the gRPC call seam for the text output path. Production callers
// pass client.WhoAmI; tests pass a stub.
type whoamiFn func(ctx context.Context, req *pb.WhoAmIRequest) (*pb.WhoAmIResponse, error)

// runWhoamiText prints the logged-in email as plain text without launching the
// TUI. It is the CI-friendly path invoked when --output text is set.
func runWhoamiText(ctx context.Context, cfg clientConfig, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()
	whoami := func(ctx context.Context, req *pb.WhoAmIRequest) (*pb.WhoAmIResponse, error) {
		return client.WhoAmI(ctx, req)
	}
	return whoamiText(ctx, cfg, out, whoami)
}

// whoamiText is the testable core of the text output path. The whoamiFn
// parameter is the seam: production passes client.WhoAmI, tests pass a stub.
// It prints just the email and a newline.
func whoamiText(ctx context.Context, cfg clientConfig, out io.Writer, whoami whoamiFn) error {
	authCtx := authContext(ctx, cfg.Token)
	resp, err := whoami(authCtx, &pb.WhoAmIRequest{})
	if err != nil {
		return fmt.Errorf("whoami: %w", err)
	}
	fmt.Fprintln(out, resp.Email)
	return nil
}
