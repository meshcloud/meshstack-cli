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

	Version      int    `json:"version"`
	FilePath     string `json:"-"`
	cacheLockers map[credential.Name]cacheLocker
}

type cacheLocker struct {
	lock.Locker

	CacheFilePath string
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
		return
	}

	out.cacheLockers = make(map[credential.Name]cacheLocker, len(credential.Names))
	for _, credentialName := range credential.Names {
		cacheFilePath := p.ConfigDir.CredentialsCacheJsonFor(p.Name, credentialName)
		locker := cacheLocker{lock.New(cacheFilePath), cacheFilePath}
		out.cacheLockers[credentialName] = locker
		if cred := out.ByName(credentialName); cred != nil {
			if err = locker.loadCache(ctx, cred); err != nil {
				return
			}
		}
	}
	return
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

func (l cacheLocker) loadCache(ctx context.Context, cred credential.Credential) error {
	return l.WithRLock(ctx, func() error {
		_, err := l.readCache(ctx, cred)
		return err
	})
}

// readCache decodes the cache of cred, leaving it untouched when the file holds one minted for
// another identity.
func (l cacheLocker) readCache(ctx context.Context, cred credential.Credential) (onDisk bool, err error) {
	var file cacheFile
	err = json.UnmarshalFrom(ctx, l.CacheFilePath, &file)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
		return
	} else if err != nil {
		return
	}
	onDisk = true

	switch {
	case file.Version != credentialsVersion:
		err = fmt.Errorf("credentials cache file %s has version %d != %d", l.CacheFilePath, file.Version, credentialsVersion)
	case file.Identity != cred.Identity().Hash:
		slog.WarnContext(ctx, fmt.Sprintf("Discarding cache %s, it was minted for another identity", l.CacheFilePath))
	default:
		credential.WithCacheOf(cred, func(cache reflect.Value) {
			err = json.Unmarshal(file.Cache, cache.Addr().Interface())
		})
	}
	return
}

func (l cacheLocker) writeCache(ctx context.Context, cred credential.Credential) (err error) {
	file := cacheFile{
		Version:  credentialsVersion,
		Identity: cred.Identity().Hash,
	}
	credential.WithCacheOf(cred, func(cache reflect.Value) {
		file.Cache, err = json.Marshal(cache.Interface())
	})
	if err != nil {
		return err
	}
	return json.MarshalTo(ctx, l.CacheFilePath, file, json.UserOnlyFilePerms())
}

func (c Credentials) ReadCache(ctx context.Context, cred credential.Credential, action func() error) (err error) {
	if !credential.WithCacheOf(cred, func(_ reflect.Value) {
		err = c.cacheLockers[c.NameOf(cred)].WithRLock(ctx, func() error {
			return action()
		})
	}) {
		err = action()
	}
	return
}

func (c Credentials) ModifyCache(ctx context.Context, cred credential.Credential, action func() error) (err error) {
	if !credential.WithCacheOf(cred, func(_ reflect.Value) {
		locker := c.cacheLockers[c.NameOf(cred)]
		err = locker.WithLock(ctx, func() (err error) {
			// The exclusive lock is held anyway, so pick up what another process wrote first. A
			// cache is only written back when one is already on disk: a caller that never stored
			// its credentials gets no file as a side effect of using them.
			onDisk, err := locker.readCache(ctx, cred)
			if err != nil {
				return err
			}
			// A failed action still gets its cache written, because it may have rotated a refresh
			// token before failing, see [credential.OidcLogin.RefreshCachedToken].
			err = action()
			if onDisk {
				if writeErr := locker.writeCache(ctx, cred); writeErr != nil {
					slog.DebugContext(ctx, fmt.Sprintf("Cannot update cache %s, continuing with the token in memory: %s", locker.CacheFilePath, writeErr.Error()))
				}
			}
			return
		})
	}) {
		err = action()
	}
	return
}

func (c Credentials) Store(ctx context.Context) error {
	var errs []error
	errs = append(errs, json.MarshalTo(ctx, c.FilePath, c, json.UserOnlyFilePerms()))
	for _, credentialName := range credential.Names {
		cred := c.ByName(credentialName)
		if cred == nil {
			continue
		}

		locker := c.cacheLockers[credentialName]
		credential.WithCacheOf(cred, func(cache reflect.Value) {
			if cache.IsNil() {
				return
			}
			errs = append(errs, locker.WithLock(ctx, func() error {
				return locker.writeCache(ctx, cred)
			}))
		})
	}
	return errors.Join(errs...)
}
