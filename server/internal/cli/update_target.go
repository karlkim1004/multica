package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateBinaryUpdateTarget protects macOS bundle signatures and their TCC identity.
// A bundle must be updated and signed as a whole by its owning installer.
func validateBinaryUpdateTarget(goos, exePath string) error {
	if goos != "darwin" {
		return nil
	}
	macOSDir := filepath.Dir(filepath.Clean(exePath))
	contentsDir := filepath.Dir(macOSDir)
	bundleDir := filepath.Dir(contentsDir)
	if filepath.Base(macOSDir) == "MacOS" && filepath.Base(contentsDir) == "Contents" &&
		strings.HasSuffix(strings.ToLower(filepath.Base(bundleDir)), ".app") {
		return fmt.Errorf("cannot replace an executable inside macOS app bundle %q: use the app's signed bundle installer to preserve its code signature and privacy permissions", bundleDir)
	}
	return nil
}
