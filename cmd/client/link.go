package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var linkCmd = &cobra.Command{
	Use:   "link <slug>",
	Short: "Link the current directory to an existing app",
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("missing app slug — use '%s link <slug>' (find your apps with '%s list')", binaryName, binaryName)
		}
		if len(args) > 1 {
			return fmt.Errorf("too many arguments — usage: '%s link <slug>'", binaryName)
		}
		return nil
	},
	RunE: runLink,
}

func runLink(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(".deploy.yaml"); err == nil {
		return fmt.Errorf("app already linked in this directory — remove .deploy.yaml first to re-link")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Token == "" {
		return fmt.Errorf("not logged in: run '%s login' first", binaryName)
	}

	m := newLinkModel(args[0], cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	lm, ok := final.(linkModel)
	if !ok || lm.err != nil {
		if ok {
			return lm.err
		}
		return fmt.Errorf("unexpected error")
	}

	if err := writeAppFile(appFile{
		Slug:         lm.slug,
		Domain:       lm.domain,
		Server:       cfg.ServerAddress,
		CreatedAt:    lm.createdAt,
		CustomDomain: lm.customDomain,
	}); err != nil {
		return err
	}

	if lm.customDomain != "" {
		fmt.Printf("Linked app: %s\n", lm.customDomain)
	} else {
		fmt.Printf("Linked app: %s.%s\n", lm.slug, lm.domain)
	}
	fmt.Printf("Run '%s push' to deploy this directory.\n", binaryName)
	return nil
}
