package profile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
	"github.com/meshcloud/meshstack-cli/internal/json"
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

func (p Profile) moveCredentialsTo(ctx context.Context, edited Profile) error {
	if !p.Endpoint.Equal(edited.Endpoint) {
		return p.RemoveCredentials(ctx)
	}
	if p.Name == edited.Name {
		return nil
	}
	credentials, err := p.Credentials(ctx)
	if err != nil {
		return err
	}
	var none Credentials
	if credentials.Credentials != none.Credentials {
		credentials.FilePath = p.ConfigDir.CredentialsJsonFor(edited.Name)
		if err := credentials.Store(ctx); err != nil {
			return err
		}
	}
	// The token caches go as well, and new tokens are minted on next use.
	return p.RemoveCredentials(ctx)
}
