package profile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/lock"
)

// ErrInUse is what LoadProfiles returns with ExclusiveLock while another command holds the
// profiles.
var ErrInUse = errors.New("another meshstack command is changing the profiles")

// lockWaitTime is short, as a holder keeps the lock for as long as a person takes: meshstack
// profile until it quits, and a login until the browser comes back.
const lockWaitTime = time.Second

func lockExclusively(ctx context.Context, dir config.Directory) (func() error, error) {
	// lock.Locker takes no lock in a directory that does not exist, and a first login creates it
	// only once it stores the profiles.
	if err := os.MkdirAll(string(dir), 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the configuration directory %s: %w", dir, err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, lockWaitTime)
	defer cancel()
	unlock, err := lock.New(dir.ProfilesJson()).Lock(waitCtx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, ErrInUse
	}
	return unlock, err
}
