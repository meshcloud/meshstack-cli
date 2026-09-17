package browser

import (
	"context"
	"os/exec"
)

// xdg-open comes from xdg-utils, a freedesktop.org tool, so it is not a sensible default beyond
// the desktops that ship it. The BSDs have it too and could share this file, but they are not
// release targets, so they fail to build here rather than being supported untested.
func execBrowserOpen(ctx context.Context, authURL string) *exec.Cmd {
	//nolint:gosec // the URL is this process's own authorization request, not input
	return exec.CommandContext(ctx, "xdg-open", authURL)
}
