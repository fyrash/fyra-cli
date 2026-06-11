package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
	customDomainResp, err := client.SetCustomDomain(ctx, &pb.SetCustomDomainRequest{
		SlugName:  af.Slug,
		Domain:    domain,
		AppDomain: af.Domain,
	})
	if err != nil {
		code := status.Code(err)
		if code == codes.FailedPrecondition {
			fmt.Printf("Deploy your app first with '%s push'.\n", binaryName)
			return nil
		}
		if code == codes.ResourceExhausted || code == codes.PermissionDenied {
			fmt.Print(tui.PlanLimitBlock(status.Convert(err).Message()))
			return nil
		}
		return fmt.Errorf("set custom domain: %w", err)
	}

	cnameAddr := customDomainResp.CnameAddr
	serverIP := customDomainResp.ServerIp

	af.CustomDomain = domain
	if err := writeAppFile(af); err != nil {
		return fmt.Errorf("update .deploy.yaml: %w", err)
	}

	fmt.Printf("Custom domain set to %s.\n", domain)
	fmt.Println()
	fmt.Printf("  Add a CNAME record:\n")
	fmt.Printf("    %s → %s\n", cnameAddr, domain)
	fmt.Println()
	fmt.Printf("  (or an A record to %s if your DNS provider doesn't support CNAME at apex)\n", serverIP)
	fmt.Println()
	fmt.Printf("Press Enter when you've added the record, or 'q' to finish later... ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "q" {
		fmt.Printf("Run '%s check-domains' when your DNS is ready.\n", binaryName)
		return nil
	}

	// Activate the cert job (waiting → pending) and start polling.
	fmt.Println("Activating certificate provisioning...")
	getStatus := func() (*pb.GetCertStatusResponse, error) {
		return client.GetCertStatus(ctx, &pb.GetCertStatusRequest{
			SlugName:  af.Slug,
			Domain:    domain,
			AppDomain: af.Domain,
		})
	}
	if err := pollCertStatus(ctx, getStatus); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled — try '%s check-domains' again later", binaryName)
		}
		return err
	}

	// Verify HTTPS reachability.
	httpClient := &http.Client{Timeout: 10 * time.Second}
	siteURL := "https://" + domain
	for attempt := range 3 {
		if attempt > 0 {
			fmt.Printf("Retrying HTTPS check (%d/2)...\n", attempt)
			time.Sleep(5 * time.Second)
		}
		resp, err := httpClient.Get(siteURL)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("✓ Custom domain is live at https://%s\n", domain)
			return nil
		}
	}
	return fmt.Errorf("domain is configured but not yet reachable. Try again in a minute")
}
