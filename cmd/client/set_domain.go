package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var setDomainCmd = &cobra.Command{
	Use:   "set-domain <domain>",
	Short: "Set a custom domain for this app",
	Args:  cobra.ExactArgs(1),
	RunE:  runSetDomain,
}

func runSetDomain(cmd *cobra.Command, args []string) error {
	domain := args[0]
	if domain == "" {
		return fmt.Errorf("domain must not be empty")
	}

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
		Domain:    domain,
		AppDomain: af.Domain,
	})
	if err != nil {
		code := status.Code(err)
		if code == codes.ResourceExhausted || code == codes.PermissionDenied {
			fmt.Print(tui.PlanLimitBlock(status.Convert(err).Message()))
			return nil
		}
		return fmt.Errorf("set custom domain: %w", err)
	}

	af.CustomDomain = domain
	if err := writeAppFile(af); err != nil {
		return fmt.Errorf("update .deploy.yaml: %w", err)
	}

	sanitisedSlug := strings.ReplaceAll(domain, ".", "-")
	fmt.Printf("Custom domain set to %s.\n", domain)
	fmt.Printf("Point your CNAME for %s → %s.%s\n", domain, sanitisedSlug, customDomainZone)
	fmt.Printf("Run '%s check-domains' to verify once DNS propagates.\n", binaryName)
	return nil
}
