// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package spring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestGetTasks_NewSpringProject validates the full initialization sequence.
func TestGetTasks_NewSpringProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}
	projectName := "test-spring-app"
	targetPath := filepath.Join(tempDir, projectName)

	plan := getPlanOrFail(t, handler, targetPath)

	assertPlanLength(t, plan, 5)
	assertCreateDirTask(t, plan[0], targetPath)
	assertCurlTask(t, plan[1], projectName)
	assertChmodTask(t, plan[4])
}

func getPlanOrFail(t *testing.T, h *Handler, path string) []types.Task {
	t.Helper()
	plan, err := h.GetTasks(path)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}
	return plan
}

func assertPlanLength(t *testing.T, plan []types.Task, expected int) {
	t.Helper()
	if len(plan) != expected {
		t.Fatalf("Expected %d tasks, got %d", expected, len(plan))
	}
}

func assertCreateDirTask(t *testing.T, task types.Task, expectedPath string) {
	t.Helper()
	dirTask, ok := task.(*tasks.CreateDirTask)
	if !ok || dirTask.Path != expectedPath {
		t.Fatal("First task should be directory creation for the target path")
	}
}

func assertCurlTask(t *testing.T, task types.Task, projectName string) {
	t.Helper()
	curlTask, ok := task.(*tasks.ExecCommandTask)
	if !ok || curlTask.Cmd != "curl" {
		t.Fatal("Second task should be 'curl'")
	}

	urlArg := findSpringURL(t, curlTask.Args)
	assertSpringURL(t, urlArg, projectName)
}

func findSpringURL(t *testing.T, args []string) string {
	t.Helper()
	for _, arg := range args {
		if strings.Contains(arg, "https://start.spring.io") {
			return arg
		}
	}
	t.Fatal("Curl task missing the Spring Initializr URL")
	return ""
}

func assertSpringURL(t *testing.T, urlArg, projectName string) {
	t.Helper()

	if !strings.Contains(urlArg, "artifactId="+projectName) {
		t.Errorf("URL missing correct artifactId. URL: %s", urlArg)
	}

	expectedPkg := "com.example.testspringapp"
	if !strings.Contains(urlArg, "packageName="+expectedPkg) {
		t.Errorf("URL missing sanitized package name. URL: %s", urlArg)
	}
}

func assertChmodTask(t *testing.T, task types.Task) {
	t.Helper()
	chmodTask, ok := task.(*tasks.ExecCommandTask)
	if !ok || chmodTask.Cmd != "chmod" {
		t.Error("Final task should be 'chmod'")
	}
}

// TestGetTasks_ExistingSpringProject validates idempotency.
func TestGetTasks_ExistingSpringProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}

	if err := os.WriteFile(
		filepath.Join(tempDir, "pom.xml"),
		[]byte("<project></project>"),
		0600, // fixed for gosec G306
	); err != nil {
		t.Fatal(err)
	}

	plan := getPlanOrFail(t, handler, tempDir)

	if len(plan) != 1 {
		t.Errorf("Expected only 1 task for existing project, got %d", len(plan))
	}
}

// TestSpringPriorityBands ensures the sequence respects the priority architecture.
func TestSpringPriorityBands(t *testing.T) {
	handler := &Handler{}
	plan, err := handler.GetTasks(filepath.Join(t.TempDir(), "app"))
	if err != nil {
		t.Fatal(err)
	}

	for i, task := range plan {
		assertPriorityBand(t, i, task.Priority())
	}
}

func assertPriorityBand(t *testing.T, index int, prio int) {
	t.Helper()

	if index == 0 {
		if prio < int(types.PrioDirCreate) || prio > int(types.MaxDirCreate) {
			t.Errorf("Dir creation task priority %d out of band", prio)
		}
		return
	}

	if prio < int(types.PrioLangSetup) || prio > int(types.MaxLangSetup) {
		t.Errorf("Lang task %d priority %d out of band", index, prio)
	}
}
