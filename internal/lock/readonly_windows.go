package lock

import (
	"errors"
	"os"
	"syscall"
)

// ERROR_WRITE_PROTECT, which syscall does not name. Its own EROFS is an invented value above
// APPLICATION_ERROR that no Windows call ever returns.
const errorWriteProtect = syscall.Errno(19)

// Unlike the other platforms, a denied permission falls back here too: Windows reports a read-only
// volume as ERROR_ACCESS_DENIED, so it cannot be told apart from an ACL denial.
func unwritableFilesystem(err error) bool {
	return errors.Is(err, errorWriteProtect) || errors.Is(err, os.ErrPermission)
}
