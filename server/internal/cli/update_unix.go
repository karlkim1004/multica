//go:build !windows

package cli

import (
	"os"
	"runtime"
)

// replaceBinary swaps the running executable for the freshly-downloaded one.
// On Unix, the kernel keeps the old inode alive for the running process, so a
// plain rename is safe for standalone binaries. App bundles require their signer.
func replaceBinary(tmpPath, exePath string) error {
	if err := validateBinaryUpdateTarget(runtime.GOOS, exePath); err != nil {
		return err
	}
	return os.Rename(tmpPath, exePath)
}

// CleanupStaleUpdateArtifacts is a no-op on Unix — there are no sidecar files
// to reclaim.
func CleanupStaleUpdateArtifacts() {}
