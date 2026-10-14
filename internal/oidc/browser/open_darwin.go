package browser

import (
	"context"
	"os/exec"
)

func execBrowserOpen(ctx context.Context, authURL string) *exec.Cmd {
	//nolint:gosec // the URL is this process's own authorization request, not input
	return exec.CommandContext(ctx, "open", authURL)
}
