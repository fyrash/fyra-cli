package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser attempts to open url in the user's default browser.
// Returns an error if no opener is available; the caller should fall back to
// printing the URL.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
