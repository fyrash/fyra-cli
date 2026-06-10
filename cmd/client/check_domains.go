package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	pb "github.com/fyrash/fyra-cli/proto/gen"
)

var checkDomainsCmd = &cobra.Command{
	Use:   "check-domains",
	Short: "Verify custom domain CNAME, TLS certificate, and HTTPS reachability",
	Args:  cobra.NoArgs,
	RunE:  runCheckDomains,
}

// cnameTarget computes the expected CNAME target for a custom domain.
func cnameTarget(domain, zone string) string {
	return strings.ReplaceAll(domain, ".", "-") + "." + zone
}

// verifyCNAMEOrARecords checks that domain points to target.
// It first tries a direct CNAME lookup (works for subdomains).
// For apex domains where Cloudflare flattens the CNAME to A records,
// it falls back to comparing resolved IPs: if domain and target share
// at least one A record, the CNAME is correctly wired.
func verifyCNAMEOrARecords(domain, target string) error {
	resolved, err := net.LookupCNAME(domain)
	if err == nil {
		resolved = strings.TrimSuffix(resolved, ".")
		if resolved == target {
			return nil
		}
	}

	// CNAME not visible — try A-record comparison (handles CNAME flattening).
	domainIPs, errD := net.LookupHost(domain)
	targetIPs, errT := net.LookupHost(target)
	if errD != nil || errT != nil {
		return fmt.Errorf("CNAME not yet propagated.\n  Expected: %s\n  Got:      %s\nTry again in a minute", target, strings.TrimSuffix(resolved, "."))
	}

	targetSet := make(map[string]bool, len(targetIPs))
	for _, ip := range targetIPs {
		targetSet[ip] = true
	}
	for _, ip := range domainIPs {
		if targetSet[ip] {
			return nil
		}
	}
	return fmt.Errorf("CNAME not yet propagated.\n  Expected: %s\n  Got:      %s\nTry again in a minute", target, strings.TrimSuffix(resolved, "."))
}

type getStatusFunc func() (*pb.GetCertStatusResponse, error)

// pollCertStatus polls getStatus until the cert is COMPLETED or the context is cancelled.
// It treats FAILED the same as PENDING — the server retries provisioning on each call,
// so we keep polling until it eventually succeeds or the user hits Ctrl+C.
func pollCertStatus(ctx context.Context, getStatus getStatusFunc) error {
	fmt.Println("Waiting for certificate... (this can take a few minutes, Ctrl+C to stop)")
	var lastStatus pb.GetCertStatusResponse_Status
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := getStatus()
		if err != nil {
			return fmt.Errorf("get cert status: %w", err)
		}

		if resp.Status != lastStatus {
			fmt.Printf("Certificate status: %s\n", resp.StatusMessage)
			lastStatus = resp.Status
		} else {
			fmt.Println("Still waiting for certificate...")
		}

		if resp.Status == pb.GetCertStatusResponse_CERT_STATUS_COMPLETED {
			return nil
		}

		// Wait for next tick or cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runCheckDomains(cmd *cobra.Command, _ []string) error {
	af, err := readAppFile()
	if err != nil {
		return err
	}
	if af.CustomDomain == "" {
		return fmt.Errorf("no custom domain set. Run '%s set-domain <domain>' first", binaryName)
	}
	domain := af.CustomDomain
	target := cnameTarget(domain, customDomainZone)

	// Prompt: confirm user has deployed the app first.
	// CDN is created during push, which is required for TLS certificate provisioning.
	fmt.Printf("Have you deployed this app with '%s push'? [y/N] ", binaryName)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Printf("Please deploy your app first with '%s push'.\n", binaryName)
		fmt.Println("The CDN domain is created during deployment, which is required for TLS certificate provisioning.")
		return nil
	}

	// Prompt: confirm user has added the CNAME before we run a lookup.
	fmt.Printf("Have you added a CNAME for %s → %s? [y/N] ", domain, target)
	answer, _ = reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Printf("Please add the CNAME record before checking.\nTarget: %s\n", target)
		return nil
	}


	// Connect to server to trigger cert retry and check status.
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

	// Poll cert status until completed or user cancels with Ctrl+C.
	// The GetCertStatus call automatically triggers a cert provisioning retry
	// (touch) on the server side, so this kickstarts the process on first poll.
	getStatus := func() (*pb.GetCertStatusResponse, error) {
		return client.GetCertStatus(ctx, &pb.GetCertStatusRequest{
			SlugName:  af.Slug,
			Domain:    domain,
			AppDomain: af.Domain,
		})
	}
	if err := pollCertStatus(ctx, getStatus); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled — try 'fyra check-domains' again later")
		}
		return err
	}

	// HTTP verification — confirm the site is reachable over HTTPS.
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
