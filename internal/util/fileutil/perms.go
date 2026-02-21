// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package fileutil provides general utility functions related to file system operations.
package fileutil

import "os"

// DirPerms is the secure permission setting recommended for directories (0750).
const DirPerms os.FileMode = 0o750
