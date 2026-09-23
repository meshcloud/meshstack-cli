package internal

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
)

const DefaultTimeout = 45 * time.Second

// RunWith runs action under its own deadline, replacing whatever deadline ctx carries.
func RunWith(ctx context.Context, timeout time.Duration, action func(context.Context) error) error {
	ctx, stopSignals := detach(ctx)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return action(ctx)
}

// RunPaged runs a command that pages through a listing. A listing takes as long as it is long, so
// no deadline bounds the command as a whole: resolving the client and each page are bounded by
// DefaultTimeout instead, which still gives up on a server that stopped answering.
func RunPaged(ctx context.Context, action func(ctx context.Context, meshStack client.Client) error) error {
	ctx, stopSignals := detach(ctx)
	defer stopSignals()
	meshStack, err := resolveClientWithin(ctx, DefaultTimeout)
	if err != nil {
		return err
	}
	return action(client.WithListOptions(ctx, client.ListOptions{PageTimeout: DefaultTimeout}), meshStack)
}

func resolveClientWithin(ctx context.Context, timeout time.Duration) (client.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return ResolveClient(ctx)
}

// detach frees ctx from the deadline and cancellation of its caller, the root command's included,
// and keeps only Ctrl-C.
func detach(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.WithoutCancel(ctx), os.Interrupt)
}
