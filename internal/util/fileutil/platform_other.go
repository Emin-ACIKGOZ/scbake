// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package fileutil

import "os"

// oNoFollow is unsupported on this platform. The value is intentionally zero so
// the safe open helpers still compile; symlinked final components are not
// rejected through the open flags here (the symlink regression tests are skipped
// on these platforms).
const oNoFollow = 0

// hardlinkCount is unavailable on this platform, so hard-link detection is a
// no-op.
func hardlinkCount(_ os.FileInfo) (uint64, bool) {
	return 0, false
}
