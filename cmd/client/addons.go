package main

import "github.com/spf13/cobra"

var addonsCmd = &cobra.Command{
	Use:   "addons",
	Short: "Manage addons for an app",
}

func init() {
	addonsCmd.AddCommand(addonsCreateCmd)
	addonsCmd.AddCommand(addonsDestroyCmd)
	addonsCmd.AddCommand(addonsListCmd)
	addonsCmd.AddCommand(addonsInfoCmd)
}
