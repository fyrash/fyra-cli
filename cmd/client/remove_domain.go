package main

import (
	"fmt"

	"github.com/spf13/cobra"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var removeDomainCmd = &cobra.Command{
	Use:   "remove-domain",
	Short: "Remove the custom domain from this app",
	Args:  cobra.NoArgs,
	RunE:  runRemoveDomain,
}

func runRemoveDomain(cmd *cobra.Command, _ []string) error {
	af, err := readAppFile()
	if err != nil {
		return err
	}

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
	_, err = client.SetCustomDomain(ctx, &pb.SetCustomDomainRequest{
		SlugName:  af.Slug,
		Domain:    "",
		AppDomain: af.Domain,
	})
	if err != nil {
		return fmt.Errorf("remove custom domain: %w", err)
	}

	af.CustomDomain = ""
	if err := writeAppFile(af); err != nil {
		return fmt.Errorf("update .deploy.yaml: %w", err)
	}
	fmt.Println("Custom domain removed.")
	return nil
}
