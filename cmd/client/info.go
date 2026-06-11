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

func runInfo(cmd *cobra.Command, _ []string) error {
	af, err := readAppFile()
	if err != nil {
		return err
	}

	// Register this app's local path in the index.
	absPath, _ := absCwd()
	if absPath != "" && af.Slug != "" {
		_ = appindex.Register(af.Slug, absPath)
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	m := newInfoModel(af.Slug, af.Domain, cfg, cmd.Context())
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
