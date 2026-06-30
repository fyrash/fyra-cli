package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var deleteAppName string

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an app",
	RunE:  runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteAppName, "appname", "", "app slug to delete (overrides .deploy.yaml)")
	deleteCmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt (non-interactive mode)")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	var slug, appDomain string
	if deleteAppName != "" {
		slug = deleteAppName
	} else {
		af, err := readAppFile()
		if err != nil {
			return err
		}
		slug = af.Slug
		appDomain = af.Domain
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	// Non-interactive mode: --yes skips the confirmation TUI.
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return runDeleteNonInteractive(cmd.Context(), cfg, slug, appDomain, os.Stdout)
	}

	m := newDeleteModel(slug, appDomain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	dm, ok := final.(deleteModel)
	if !ok || dm.err != nil {
		if ok {
			return dm.err
		}
		return fmt.Errorf("unexpected error")
	}

	if !dm.confirmed {
		fmt.Println("Cancelled.")
		return nil
	}

	// Remove .deploy.yaml if it belongs to the deleted app.
	_ = removeAppFile()

	return nil
}

// deleteFn is the gRPC call seam for the non-interactive delete path. It takes
// the already-authenticated context and request and returns the server
// response. Production callers pass client.DeleteApp; tests pass a stub.
type deleteFn func(ctx context.Context, req *pb.DeleteAppRequest) (*pb.DeleteAppResponse, error)

// runDeleteNonInteractive deletes an app without launching the confirmation
// TUI. It is the CI-friendly path invoked when --yes is set.
func runDeleteNonInteractive(ctx context.Context, cfg clientConfig, slug, appDomain string, out io.Writer) error {
	client, cleanup, err := cfg.dial()
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer cleanup()
	del := func(ctx context.Context, req *pb.DeleteAppRequest) (*pb.DeleteAppResponse, error) {
		return client.DeleteApp(ctx, req)
	}
	return deleteNonInteractive(ctx, cfg, slug, appDomain, out, del)
}

// deleteNonInteractive is the testable core of the non-interactive delete path.
// The deleteFn parameter is the seam: production passes client.DeleteApp,
// tests pass a stub — no real network needed.
func deleteNonInteractive(
	ctx context.Context,
	cfg clientConfig,
	slug, appDomain string,
	out io.Writer,
	del deleteFn,
) error {
	authCtx := authContext(ctx, cfg.Token)
	resp, err := del(authCtx, &pb.DeleteAppRequest{
		SlugName: slug,
		Domain:   appDomain,
	})
	if err != nil {
		return friendlyDeleteError(err)
	}

	deletedSlug := slug
	if resp != nil && resp.SlugName != "" {
		deletedSlug = resp.SlugName
	}
	fmt.Fprintf(out, "App %s deleted.\n", deletedSlug)

	// Remove .deploy.yaml if it belongs to the deleted app.
	_ = removeAppFile()
	return nil
}
