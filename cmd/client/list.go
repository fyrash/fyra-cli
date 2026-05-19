package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all your apps",
	RunE:  runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
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
