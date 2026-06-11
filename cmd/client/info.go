package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
	"github.com/fyrash/fyra-cli/internal/appindex"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show details about the app in the current directory",
	RunE:  runInfo,
}

func init() {
	infoCmd.Flags().String("app", "", "app slug (default: read from .deploy.yaml)")
}

func runInfo(cmd *cobra.Command, _ []string) error {
	var slug, appDomain string

	appFlag, _ := cmd.Flags().GetString("app")
	if appFlag != "" {
		slug = appFlag
	} else {
		af, err := readAppFile()
		if err != nil {
			return err
		}
		slug = af.Slug
		appDomain = af.Domain
	}

	// Register this app's local path in the index.
	absPath, _ := absCwd()
	if absPath != "" && slug != "" {
		_ = appindex.Register(slug, absPath)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newInfoModel(slug, appDomain, cfg, cmd.Context())
	final, err := tui.Run(m)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	im, ok := final.(infoModel)
	if !ok || im.err != nil {
		if ok {
			return im.err
		}
		return fmt.Errorf("unexpected error")
	}
	return nil
}
