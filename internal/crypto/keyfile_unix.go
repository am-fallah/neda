//go:build unix

package crypto

import (
	"fmt"
	"os"
	"syscall"
)

// checkKeyFileOwnership requires the key file to be owned by the runtime user.
// Group/other permission bits are already required to be zero, so group
// ownership is not security-relevant on its own.
func checkKeyFileOwnership(info os.FileInfo) error {
	return checkKeyFileOwner(info, os.Geteuid())
}

func checkKeyFileOwner(info os.FileInfo, euid int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("key file ownership cannot be verified")
	}
	if int(stat.Uid) != euid {
		return fmt.Errorf("key file must be owned by the runtime user (uid %d)", euid)
	}
	return nil
}
