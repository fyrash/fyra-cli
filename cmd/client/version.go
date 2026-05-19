package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build version and check for updates",
	Run: func(_ *cobra.Command, _ []string) {
		latest, err := fetchLatestVersion(versionURL)
		if err != nil || !isNewer(version, latest) {
			fmt.Printf("%s %s\n", binaryName, version)
			return
		}
		fmt.Printf("%s %s (latest: %s — run '%s update' to upgrade)\n", binaryName, version, latest, binaryName)
	},
}
