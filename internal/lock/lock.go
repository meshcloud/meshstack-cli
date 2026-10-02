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
// preempt a steady stream of readers. The token cache relies on every holder releasing quickly,
// as a read is one field and a write is one token refresh. A holder of Lock may keep it for as
// long as a person takes, so whoever waits for such a lock gives up after a short timeout.
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

// Lock holds the exclusive lock until unlock, which releases it on its first call only.
func (l Locker) Lock(ctx context.Context) (unlock func() error, err error) {
	return l.lock(ctx, false)
}

func (l Locker) with(ctx context.Context, read bool, fn func() error) (err error) {
	unlock, err := l.lock(ctx, read)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, unlock())
	}()
	return fn()
}

func (l Locker) lock(ctx context.Context, read bool) (unlock func() error, err error) {
	inMemory := l.inMemoryLocker(read)
	if err := inMemory.SpinLock(ctx); err != nil {
		return nil, err
	}
	file := l.fileLocker(read)
	if err := file.SpinLock(ctx); err != nil {
		return nil, errors.Join(err, inMemory.Unlock())
	}
	return sync.OnceValue(func() error {
		return errors.Join(file.Unlock(), inMemory.Unlock())
	}), nil
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
