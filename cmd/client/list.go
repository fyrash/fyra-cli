package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all your apps",
	RunE:  runList,
}

func init() {
	listCmd.Flags().String("output", "", "output format (text); default is the interactive TUI")
}

func runList(cmd *cobra.Command, _ []string) error {
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
		return runListText(cmd.Context(), cfg, cmd.OutOrStdout())
	}

	m := newListModel(cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	if lm, ok := final.(listModel); ok && lm.err != nil {
		return lm.err
	}
	return nil
}

// listAppsFn is the gRPC call seam for the text output path. Production callers
// pass client.ListApps; tests pass a stub.
type listAppsFn func(ctx context.Context, req *pb.ListAppsRequest) (*pb.ListAppsResponse, error)

// runListText lists apps as plain text without launching the TUI. It is the
// CI-friendly path invoked when --output text is set.
func runListText(ctx context.Context, cfg clientConfig, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()
	list := func(ctx context.Context, req *pb.ListAppsRequest) (*pb.ListAppsResponse, error) {
		return client.ListApps(ctx, req)
	}
	return listText(ctx, cfg, out, list)
}

// listText is the testable core of the text output path. The listAppsFn
// parameter is the seam: production passes client.ListApps, tests pass a stub.
//
// Each app is printed as a tab-separated row: slug<TAB>domain<TAB>url<TAB>created_at.
// The domain column is the app's domain zone; url is the fully-qualified URL
// (empty if never deployed).
func listText(ctx context.Context, cfg clientConfig, out io.Writer, list listAppsFn) error {
	authCtx := authContext(ctx, cfg.Token)
	resp, err := list(authCtx, &pb.ListAppsRequest{})
	if err != nil {
		return fmt.Errorf("list apps: %w", err)
	}

	for _, a := range resp.Apps {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", a.SlugName, a.Domain, a.Url, truncDate(a.CreatedAt))
	}
	return nil
}

// truncDate returns the date portion (first 10 chars) of an RFC3339 timestamp,
// or the whole string if it is shorter.
func truncDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return strings.TrimSpace(s)
}
