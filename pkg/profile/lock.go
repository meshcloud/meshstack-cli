package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/diags"
)

// The lock is a file created with O_CREATE|O_EXCL next to the credentials file, rather
// than syscall.Flock: the CLI releases for Linux, macOS and Windows through goreleaser,
// and flock does not exist on Windows.
//
// It matters that it works everywhere, because the two processes it serialises are a
// `terraform apply` and a `meshstack` command renewing from the same profile. Keycloak
// rotates the refresh token on every refresh and ends the whole session once one is
// reused too often, so two unsynchronised refresh grants cost the user their login.
const lockSuffix = ".lock"

const (
	// Short enough that the common case — the other holder is one HTTP round trip from
	// done — is not noticeably delayed, and backed off so a long hold costs few syscalls.
	lockRetryInitial = 20 * time.Millisecond
	lockRetryMax     = 250 * time.Millisecond

	// A lock older than this is treated as abandoned. It is comfortably longer than a
	// token round trip, and short enough that a process killed mid-renewal does not lock
	// a user out until they find the file themselves.
	lockStaleAfter = 60 * time.Second
)

// acquireLock takes the exclusive lock for a credentials file, waiting until it can or
// until ctx is done. The returned function releases it.
func acquireLock(ctx context.Context, path string) (func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, diags.Wrap(err, "Cannot create the credentials directory",
			"%s could not be created.", dir)
	}

	backoff := lockRetryInitial
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err == nil {
			// The holder's pid is written for a human debugging a stuck lock; nothing reads it.
			_, _ = fmt.Fprintf(f, "pid %d at %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = f.Close()
			return func() {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("could not release the credentials lock", "file", path, "error", err)
				}
			}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, diags.Wrap(err, "Cannot lock the credentials",
				"%s could not be created. The credentials directory has to be writable, because a token renewal has to be serialised against other meshStack processes.", path)
		}
		if breakStaleLock(path) {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, diags.Wrap(ctx.Err(), "Timed out waiting for the credentials lock",
				"Another meshStack process is holding %s. If none is running, remove the file.", path)
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > lockRetryMax {
			backoff = lockRetryMax
		}
	}
}

// breakStaleLock removes a lock whose holder is long gone, and reports whether it did.
// Two waiters may break the same lock and then both create one, which is why the window
// is generous: the loss is a duplicate renewal, not a corrupt file, because the write
// itself is atomic.
func breakStaleLock(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// Gone already, or unreadable. Either way the caller should just retry the create.
		return errors.Is(err, fs.ErrNotExist)
	}
	age := time.Since(info.ModTime())
	if age < lockStaleAfter {
		return false
	}
	if err := os.Remove(path); err != nil {
		slog.Warn("could not break a stale credentials lock", "file", path, "age", age, "error", err)
		return false
	}
	slog.Warn("broke a stale credentials lock left behind by another process", "file", path, "age", age)
	return true
}
