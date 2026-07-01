package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// binaryName is the name shown in help text and user-facing messages.
// Override at build time: -ldflags "-X main.binaryName=fyra"
var binaryName = "fyra"

// version is the build version, injected at build time.
// Override at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

// customDomainZone is used for CNAMEs to point custom domains to
var customDomainZone = "ignite.fyra.sh"

var cfgFile string

var rootCmd = &cobra.Command{
	Use:           binaryName,
	Short:         "fyra.sh — deploy static sites from the command line",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPostRun: func(cmd *cobra.Command, _ []string) {
		if cmd.Name() == "update" || cmd.Name() == "version" {
			return
		}
		go checkForUpdate()
	},
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.fyra/config.yaml)")
	rootCmd.PersistentFlags().String("server", "server.fyra.sh:50052", "override server address (env: DEPLOY_SERVER)")
	rootCmd.AddCommand(registerCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(confirmCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(setDomainCmd)
	rootCmd.AddCommand(removeDomainCmd)
	rootCmd.AddCommand(checkDomainsCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(addonsCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(resetPasswordCmd)
	rootCmd.AddCommand(infoCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
