package browser

import (
	"context"
	"os/exec"
)

// xdg-open comes from xdg-utils, so it is only there on the desktops that ship freedesktop.org
// tools. The BSDs have it too, but they are no release target and get no file of their own.
func execBrowserOpen(ctx context.Context, authURL string) *exec.Cmd {
	//nolint:gosec // the URL is this process's own authorization request, not input
	return exec.CommandContext(ctx, "xdg-open", authURL)
}
