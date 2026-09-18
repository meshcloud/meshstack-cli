package internal

import (
	"context"
	"os"
	"os/signal"
	"time"
)

const DefaultTimeout = 45 * time.Second

func RunWith(ctx context.Context, timeout time.Duration, action func(context.Context) error) error {
	ctx, stopSignals := signal.NotifyContext(context.WithoutCancel(ctx), os.Interrupt)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return action(ctx)
}
