package auth

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"reflect"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/lock"
	"github.com/meshcloud/meshstack-cli/internal/profile"
)

const cacheVersion = 1

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
	// Stored is set for the credential the profile stores, and not for one that settings provide.
	Stored bool
	// bringsItsToken is set for an API token that settings provide. A manual credential's identity
	// is its endpoint alone, so the profile's cache would match it and replace that token with the
	// one a login stored.
	bringsItsToken bool

	profile   profile.Name
	locker    lock.Locker
	cachePath string
}

func CacheFor(p *profile.Profile, cred credential.Credential) Credential {
	path := p.ConfigDir.CredentialsCacheJsonFor(p.Name, cred.Name())
	return Credential{Credential: cred, profile: p.Name, locker: lock.New(path), cachePath: path}
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
				slog.DebugContext(ctx, fmt.Sprintf("Cannot update cache %s, continuing with the token in memory: %s", c.cachePath, writeErr.Error()))
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

func (c Credential) read(ctx context.Context) (matched bool, err error) {
	if c.bringsItsToken {
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
		slog.WarnContext(ctx, fmt.Sprintf("Ignoring cache %s of version %d, as this meshstack reads version %d; run 'meshstack login -p %s' to update it",
			c.cachePath, file.Version, cacheVersion, c.profile))
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
