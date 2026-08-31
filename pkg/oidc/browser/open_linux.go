package browser

import "os/exec"

// xdg-open comes from xdg-utils, a freedesktop.org tool, so it is not a sensible default beyond
// the desktops that ship it. The BSDs have it too and could share this file, but they are not
// release targets, so they fail to build here rather than being supported untested.
func execBrowserOpen(authURL string) *exec.Cmd {
	return exec.Command("xdg-open", authURL)
}
