package auth

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"reflect"
	"sync"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/lock"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

const cacheVersion = 1

var warnOfFailedCacheWrite sync.Once

// cacheFile carries the identity the cached tokens were minted for, so that a cache
// written by another api key or after a rotated secret is dropped instead of sent.
type cacheFile struct {
	Version  int            `json:"version"`
	Identity string         `json:"identity"`
	Cache    jsontext.Value `json:"cache,omitzero"`
}

// Credential is the credential of a Session, with where it came from, and its token cache, shared
// with every other process using the same profile and credential.
type Credential struct {
	credential.Credential

	// Sources must never carry a secret, as a status shows them.
	Sources []string

	locker    lock.Locker
	cachePath string
}

func CacheFor(p *profile.Profile, cred credential.Credential) Credential {
	var path string
	if _, isToken := cred.(*credential.Manual); !isToken && p.Name != "" {
		path = p.ConfigDir.CredentialsCacheJsonFor(p.Name, cred.Name())
	}
	return Credential{Credential: cred, locker: lock.New(path), cachePath: path}
}

func (c Credential) Load(ctx context.Context) error {
	return c.locker.WithRLock(ctx, func() error {
		_, err := c.read(ctx)
		return err
	})
}

func (c Credential) Read(ctx context.Context, action func() error) error {
	return c.locker.WithRLock(ctx, action)
}

func (c Credential) Modify(ctx context.Context, action func() error) error {
	return c.locker.WithLock(ctx, func() error {
		// The exclusive lock is held anyway, so pick up what another process wrote first. The
		// cache is only written back over a file minted for this very credential: one from the
		// environment leaves the profile's own cache alone, and one never stored gets no file.
		matched, err := c.read(ctx)
		if err != nil {
			return err
		}
		// A failed action still gets its cache written, because it may have rotated a refresh
		// token before failing, see [credential.OidcLogin.RefreshCachedToken].
		err = action()
		if matched {
			if writeErr := c.write(ctx); writeErr != nil {
				warnOfFailedCacheWrite.Do(func() {
					slog.WarnContext(ctx, fmt.Sprintf("Cannot update cache %s, continuing with the token in memory: %s", c.cachePath, writeErr.Error()))
				})
			}
		}
		return err
	})
}

// Write stores the cache even when it is nil, because the identity alone is what lets a later
// Modify write back the token it mints.
func (c Credential) Write(ctx context.Context) error {
	return c.locker.WithLock(ctx, func() error {
		return c.write(ctx)
	})
}

// WriteOver is Write for a login, whose restore puts back the cache file it replaced, or removes the
// one it created, so that a failed login leaves no token behind. The file it replaced may belong to
// an earlier login of the profile, which stays stored when the new one fails.
func (c Credential) WriteOver(ctx context.Context) (restore func(context.Context) error, err error) {
	var previous []byte
	err = c.locker.WithLock(ctx, func() error {
		previous, err = os.ReadFile(c.cachePath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return c.write(ctx)
	})
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		return c.locker.WithLock(ctx, func() error {
			if previous == nil {
				if err := os.Remove(c.cachePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				return nil
			}
			return json.MarshalTo(ctx, c.cachePath, jsontext.Value(previous), json.UserOnlyFilePerms())
		})
	}, nil
}

func (c Credential) read(ctx context.Context) (matched bool, err error) {
	if c.cachePath == "" {
		return false, nil
	}
	var file cacheFile
	err = json.UnmarshalFrom(ctx, c.cachePath, &file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return
	}

	switch {
	case file.Version != cacheVersion:
		// Not an error: the cache only saves minting a token, and a login writes a compatible one.
		slog.WarnContext(ctx, fmt.Sprintf("Ignoring cache %s of version %d, as this meshstack reads version %d; a login updates it",
			c.cachePath, file.Version, cacheVersion))
	case file.Identity != c.Identity().Hash:
		// Debug, not Warn: a credential from the environment next to the profile's own is normal.
		slog.DebugContext(ctx, fmt.Sprintf("Ignoring cache %s, it was minted for another identity", c.cachePath))
	default:
		matched = true
		credential.WithCacheOf(c.Credential, func(cache reflect.Value) {
			err = json.Unmarshal(file.Cache, cache.Addr().Interface())
		})
	}
	return
}

func (c Credential) write(ctx context.Context) (err error) {
	if c.cachePath == "" {
		return nil
	}
	file := cacheFile{
		Version:  cacheVersion,
		Identity: c.Identity().Hash,
	}
	credential.WithCacheOf(c.Credential, func(cache reflect.Value) {
		file.Cache, err = json.Marshal(cache.Interface())
	})
	if err != nil {
		return err
	}
	return json.MarshalTo(ctx, c.cachePath, file, json.UserOnlyFilePerms())
}
