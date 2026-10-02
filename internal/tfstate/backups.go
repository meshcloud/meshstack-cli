package tfstate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

type Backups string

func ResolveBackups(ctx context.Context, sources setting.Sources) (Backups, error) {
	dir, err := sources.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return "", err
	}
	return Backups(dir.Join("tfstate-backups")), nil
}

// save names the file by the time down to the nanosecond, so that two writes within one second keep
// a backup each.
func (b Backups) save(buildingBlock uuid.UUID, state []byte, at time.Time) (path string, err error) {
	if err = os.MkdirAll(string(b), 0o700); err != nil {
		return "", err
	}
	path = filepath.Join(string(b), fmt.Sprintf("%s-%s.json", buildingBlock, at.UTC().Format("20060102T150405.000000000Z")))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a file of its own, named by the uuid and the time
	if err != nil {
		return "", err
	}
	_, err = file.Write(state)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return path, err
}
