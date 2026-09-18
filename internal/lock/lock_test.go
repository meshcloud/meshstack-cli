package lock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/internal/lock"
)

const settle = 250 * time.Millisecond

func noop() error { return nil }

func fileLockIsFree(t *testing.T, path string) bool {
	t.Helper()

	probe := flock.New(path + ".lock")

	taken, err := probe.TryLock()
	require.NoError(t, err)

	if taken {
		require.NoError(t, probe.Close())
	}

	return taken
}

func lockBlocked(t *testing.T, l lock.Locker) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), settle)
	defer cancel()

	var ran bool

	err := l.WithLock(ctx, func() error {
		ran = true

		return nil
	})
	if err != nil {
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.False(t, ran, "the callback must not run when the lock was not taken")

		return true
	}

	require.True(t, ran)

	return false
}

func TestLocksTheFileWhileTheCallbackRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	l := lock.New(path)
	require.NoFileExists(t, path+".lock", "New touches nothing, the file lock is taken on demand")

	require.NoError(t, l.WithLock(t.Context(), func() error {
		require.False(t, fileLockIsFree(t, path))

		return nil
	}))

	require.True(t, fileLockIsFree(t, path))
}

func TestFallsBackToTheMutexWhenTheDirectoryIsMissing(t *testing.T) {
	dir := t.TempDir()

	l := lock.New(filepath.Join(dir, "absent", "credentials"))

	require.NoError(t, l.WithLock(t.Context(), noop))
	require.NoDirExists(t, filepath.Join(dir, "absent"), "the fallback creates nothing on disk")
}

func TestAPermissionErrorBubblesInsteadOfFallingBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows cannot tell a read-only volume from an ACL denial, so both fall back there")
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permissions this test rests on")
	}

	dir := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.Mkdir(dir, 0o500))

	l := lock.New(filepath.Join(dir, "credentials"))
	require.ErrorIs(t, l.WithLock(t.Context(), noop), os.ErrPermission)
}

func TestFallbackLockerOnlyProtectsItsOwnValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "credentials")

	a := lock.New(path)
	b := lock.New(path)

	require.NoError(t, a.WithLock(t.Context(), func() error {
		require.False(t, lockBlocked(t, b),
			"with no file to lock the guarantee is process-local, so callers have to share one Locker")

		return nil
	}))
}

func TestTheCallbackErrorReachesTheCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	l := lock.New(path)

	failed := errors.New("writing the credentials failed")
	require.ErrorIs(t, l.WithLock(t.Context(), func() error { return failed }), failed)
	require.True(t, fileLockIsFree(t, path), "a failing callback still releases the lock")
}

func TestAPanickingCallbackReleasesTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	l := lock.New(path)

	require.Panics(t, func() {
		_ = l.WithLock(t.Context(), func() error { panic("boom") })
	})

	require.True(t, fileLockIsFree(t, path))
	require.NoError(t, l.WithLock(t.Context(), noop), "the mutex must be free too")
}

func TestTheLastReaderReleasesTheFileLock(t *testing.T) {
	const readers = 4

	path := filepath.Join(t.TempDir(), "credentials")

	l := lock.New(path)

	holding := make(chan struct{}, readers)
	release := make(chan struct{})
	released := make(chan struct{}, readers)

	var wg sync.WaitGroup

	for range readers {
		wg.Go(func() {
			assert.NoError(t, l.WithRLock(t.Context(), func() error {
				holding <- struct{}{}
				<-release

				return nil
			}))

			released <- struct{}{}
		})
	}

	for range readers {
		<-holding
	}

	for i := range readers {
		require.False(t, fileLockIsFree(t, path), "reader %d of %d still holds the shared lock", i+1, readers)

		release <- struct{}{}
		<-released
	}

	wg.Wait()
	require.True(t, fileLockIsFree(t, path))
}

func TestOnlyOneWriterRunsAtATime(t *testing.T) {
	l := lock.New(filepath.Join(t.TempDir(), "credentials"))

	var live, peak atomic.Int64

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			assert.NoError(t, l.WithLock(t.Context(), func() error {
				if n := live.Add(1); n > peak.Load() {
					peak.Store(n)
				}

				time.Sleep(time.Millisecond)
				live.Add(-1)

				return nil
			}))
		})
	}

	wg.Wait()
	require.Equal(t, int64(1), peak.Load())
}

func TestSeparateLockersExcludeEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	a := lock.New(path)
	b := lock.New(path)

	require.NoError(t, a.WithLock(t.Context(), func() error {
		require.True(t, lockBlocked(t, b))

		return nil
	}))

	require.False(t, lockBlocked(t, b))
}

func TestAFailedFileLockReleasesTheMutex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	a := lock.New(path)
	b := lock.New(path)

	require.NoError(t, a.WithLock(t.Context(), func() error {
		require.True(t, lockBlocked(t, b))

		return nil
	}))

	require.NoError(t, b.WithLock(t.Context(), noop),
		"b's own mutex must not still be held by the attempt that timed out")
}

func TestWaitingForTheMutexIsCancellable(t *testing.T) {
	l := lock.New(filepath.Join(t.TempDir(), "absent", "credentials"))

	require.NoError(t, l.WithLock(t.Context(), func() error {
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(settle)
			cancel()
		}()

		err := l.WithLock(ctx, func() error {
			require.Fail(t, "the callback must not run")

			return nil
		})
		require.ErrorIs(t, err, context.Canceled)

		return nil
	}))
}

func TestWaitingForTheFileIsCancellable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	a := lock.New(path)
	b := lock.New(path)

	require.NoError(t, a.WithLock(t.Context(), func() error {
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(settle)
			cancel()
		}()

		err := b.WithLock(ctx, func() error {
			require.Fail(t, "the callback must not run")

			return nil
		})
		require.ErrorIs(t, err, context.Canceled)

		return nil
	}))
}

func TestAnExpiredContextTakesNoLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")

	l := lock.New(path)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := l.WithLock(ctx, func() error {
		require.Fail(t, "the callback must not run")

		return nil
	})
	require.ErrorIs(t, err, context.Canceled)

	require.True(t, fileLockIsFree(t, path))
	require.NoError(t, l.WithLock(t.Context(), noop), "the mutex must not be left held")
}
