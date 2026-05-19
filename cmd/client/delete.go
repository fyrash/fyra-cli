package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fyrash/fyra-cli/cmd/client/tui"
)

var deleteAppName string

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an app",
	RunE:  runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteAppName, "appname", "", "app slug to delete (overrides .deploy.yaml)")
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
