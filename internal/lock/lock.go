package lock

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const retryDelay = 25 * time.Millisecond

func New(path string) Locker {
	return Locker{m: &sync.RWMutex{}, pathLock: path + ".lock"}
}

// Locker guards one file against other goroutines with an RWMutex, and against other
// processes with a lock file. Both spin on a try-lock, so neither is fair: a writer cannot
// preempt a steady stream of readers, and every holder has to release quickly. That is the
// bargain the token cache is built on — a read is one field, a write is one token refresh.
type Locker struct {
	m        *sync.RWMutex
	pathLock string
}

func (l Locker) WithLock(ctx context.Context, fn func() error) error {
	return l.with(ctx, false, fn)
}

func (l Locker) WithRLock(ctx context.Context, fn func() error) error {
	return l.with(ctx, true, fn)
}

func (l Locker) with(ctx context.Context, read bool, fn func() error) error {
	return l.inMemoryLocker(read).With(ctx, func() error {
		return l.fileLocker(read).With(ctx, fn)
	})
}

func (l Locker) inMemoryLocker(read bool) delegatingLocker {
	tryLock := l.m.TryLock
	unlock := l.m.Unlock
	if read {
		tryLock = l.m.TryRLock
		unlock = l.m.RUnlock
	}
	return delegatingLocker{
		DelegateTryLock: func() (bool, error) {
			return tryLock(), nil
		},
		DelegateUnlock: func() error {
			unlock()
			return nil
		},
	}
}

func (l Locker) fileLocker(read bool) delegatingLocker {
	//goland:noinspection GoResourceLeak
	f := flock.New(l.pathLock)

	tryLock := f.TryLock
	unlock := f.Unlock
	if read {
		tryLock = f.TryRLock
	}

	ignoreErr := func(err error) bool {
		return errors.Is(err, os.ErrNotExist) || unwritableFilesystem(err)
	}

	return delegatingLocker{
		DelegateTryLock: func() (ok bool, err error) {
			defer func() {
				if ignoreErr(err) {
					ok, err = true, nil
				}
			}()
			return tryLock()
		},
		DelegateUnlock: func() (err error) {
			defer func() {
				if ignoreErr(err) {
					err = nil
				}
			}()
			return unlock()
		},
	}
}

type (
	tryLockFunc func() (bool, error)
	unlockFunc  func() error
)

type delegatingLocker struct {
	DelegateTryLock tryLockFunc
	DelegateUnlock  unlockFunc
}

func (l delegatingLocker) With(ctx context.Context, fn func() error) (err error) {
	if lockErr := l.SpinLock(ctx); lockErr != nil {
		return lockErr
	}
	defer func() {
		err = errors.Join(err, l.Unlock())
	}()
	return fn()
}

func (l delegatingLocker) SpinLock(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if acquired, err := l.TryLock(); err != nil {
			return err
		} else if acquired {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
}

func (l delegatingLocker) TryLock() (bool, error) {
	return l.DelegateTryLock()
}

func (l delegatingLocker) Unlock() error {
	return l.DelegateUnlock()
}
