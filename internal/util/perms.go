// Package util provides general utility functions used across the scbake project.
package util

import "os"

// DirPerms is the secure permission setting recommended for directories (0750).
const DirPerms os.FileMode = 0o750
