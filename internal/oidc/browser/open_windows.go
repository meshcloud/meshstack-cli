package browser

import (
	"context"
	"os/exec"
)

// url.dll's FileProtocolHandler rather than `start`, which is a cmd.exe builtin and would need a
// shell — and a shell would treat the & in the query string as a command separator.
func execBrowserOpen(ctx context.Context, authURL string) *exec.Cmd {
	//nolint:gosec // the URL is this process's own authorization request, not input
	return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", authURL)
}
