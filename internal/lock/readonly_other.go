//go:build !windows

package lock

import (
	"errors"
	"syscall"
)

func unwritableFilesystem(err error) bool {
	return errors.Is(err, syscall.EROFS)
}
