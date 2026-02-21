// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package preflight

import (
	"fmt"
	"os/exec"
)

// CheckBinaries ensures all required binaries are in the user's $PATH.
func CheckBinaries(binaries ...string) error {
	for _, bin := range binaries {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("'%s' command not found in $PATH. Please install it to continue", bin)
		}
	}
	return nil
}
