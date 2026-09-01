package browser

import "os/exec"

func execBrowserOpen(authURL string) *exec.Cmd {
	return exec.Command("open", authURL)
}
