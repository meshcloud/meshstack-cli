package browser

import "os/exec"

// url.dll's FileProtocolHandler rather than `start`, which is a cmd.exe builtin and would need a
// shell — and a shell would treat the & in the query string as a command separator.
func execBrowserOpen(authURL string) *exec.Cmd {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", authURL)
}
