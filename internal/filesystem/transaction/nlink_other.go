// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package transaction

import "os"

// hardlinkCount is unavailable on this platform, so hard-link detection is a
// no-op.
func hardlinkCount(_ os.FileInfo) (uint64, bool) {
	return 0, false
}
