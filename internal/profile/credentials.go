package profile

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"reflect"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/lock"
)

const (
	credentialsVersion = 1
)

type Credentials struct {
	credential.Credentials

	Version  int    `json:"version"`
	FilePath string `json:"-"`
}

func (p Profile) Credentials(ctx context.Context) (out Credentials, err error) {
	out.FilePath = p.ConfigDir.CredentialsJsonFor(p.Name)
	err = json.UnmarshalFrom(ctx, out.FilePath, &out)
	if errors.Is(err, fs.ErrNotExist) {
		out.Version = credentialsVersion
		err = nil
	} else if err != nil {
		return
	} else if out.Version != credentialsVersion {
		err = fmt.Errorf("credentials file %s has version %d != %d", out.FilePath, out.Version, credentialsVersion)
	}
	return
}

func (c Credentials) Store(ctx context.Context) error {
	return json.MarshalTo(ctx, c.FilePath, c, json.UserOnlyFilePerms())
}

func (p Profile) RemoveCredentials(ctx context.Context) error {
	var errs []error
	removeFileIfPresent := func(f string) {
		err := os.Remove(f)
		if err == nil {
			slog.DebugContext(ctx, "Removed file "+f)
		} else if !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("failed to remove %s: %w", f, err))
		}
	}
	removeFileIfPresent(p.ConfigDir.CredentialsJsonFor(p.Name))
	for _, credentialName := range credential.Names {
		removeFileIfPresent(p.ConfigDir.CredentialsCacheJsonFor(p.Name, credentialName))
	}
	return errors.Join(errs...)
}

// cacheFile carries the identity the cached tokens were minted for, so that a cache
// written by another api key or after a rotated secret is dropped instead of sent.
type cacheFile struct {
	Version  int            `json:"version"`
	Identity string         `json:"identity"`
	Cache    jsontext.Value `json:"cache,omitzero"`
}

// CachedCredential is the token cache of the one credential a session uses, shared with every other process
// using the same profile and credential.
type CachedCredential struct {
	credential.Credential

	locker lock.Locker
	path   string
}

func (p Profile) CacheFor(cred credential.Credential) CachedCredential {
	path := p.ConfigDir.CredentialsCacheJsonFor(p.Name, cred.Name())
	return CachedCredential{cred, lock.New(path), path}
}

func (c CachedCredential) Load(ctx context.Context) error {
	return c.locker.WithRLock(ctx, func() error {
		_, err := c.read(ctx)
		return err
	})
}

func (c CachedCredential) Read(ctx context.Context, action func() error) error {
	return c.locker.WithRLock(ctx, action)
}

func (c CachedCredential) Modify(ctx context.Context, action func() error) error {
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
				slog.DebugContext(ctx, fmt.Sprintf("Cannot update cache %s, continuing with the token in memory: %s", c.path, writeErr.Error()))
			}
		}
		return err
	})
}

// Write stores the cache even when it is nil, because the identity alone is what lets a later
// Modify write back the token it mints.
func (c CachedCredential) Write(ctx context.Context) error {
	return c.locker.WithLock(ctx, func() error {
		return c.write(ctx)
	})
}

func (c CachedCredential) read(ctx context.Context) (matched bool, err error) {
	var file cacheFile
	err = json.UnmarshalFrom(ctx, c.path, &file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return
	}

	switch {
	case file.Version != credentialsVersion:
		err = fmt.Errorf("credentials cache file %s has version %d != %d", c.path, file.Version, credentialsVersion)
	case file.Identity != c.Identity().Hash:
		// Debug, not Warn: a credential from the environment next to the profile's own is normal.
		slog.DebugContext(ctx, fmt.Sprintf("Ignoring cache %s, it was minted for another identity", c.path))
	default:
		matched = true
		credential.WithCacheOf(c.Credential, func(cache reflect.Value) {
			err = json.Unmarshal(file.Cache, cache.Addr().Interface())
		})
	}
	return
}

func (c CachedCredential) write(ctx context.Context) (err error) {
	file := cacheFile{
		Version:  credentialsVersion,
		Identity: c.Identity().Hash,
	}
	credential.WithCacheOf(c.Credential, func(cache reflect.Value) {
		file.Cache, err = json.Marshal(cache.Interface())
	})
	if err != nil {
		return err
	}
	return json.MarshalTo(ctx, c.path, file, json.UserOnlyFilePerms())
}
