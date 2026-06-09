// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package community

import (
	"testing"
)

func TestCommunityHandler_GetTasks(t *testing.T) {
	h := &Handler{}
	tasks, err := h.GetTasks(".")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	expectedFiles := map[string]bool{
		"CONTRIBUTING.md":   true,
		"CODE_OF_CONDUCT.md": true,
		"SUPPORT.md":         true,
		"GOVERNANCE.md":      true,
	}

	if len(tasks) != len(expectedFiles) {
		t.Errorf("Expected %d tasks, got %d", len(expectedFiles), len(tasks))
	}

	for _, task := range tasks {
		desc := task.Description()
		found := false
		for file := range expectedFiles {
			if desc == "Create "+file {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected task: %s", desc)
		}
	}
}
