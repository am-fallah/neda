//go:build !unix

package crypto

import "os"

// checkKeyFileOwnership is a no-op on platforms without Unix ownership
// metadata. Permission checks still apply.
func checkKeyFileOwnership(_ os.FileInfo) error { return nil }
