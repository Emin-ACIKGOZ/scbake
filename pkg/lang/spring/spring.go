package spring

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

// GetTasks returns the execution plan for initializing a Spring Boot project at targetPath.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Determine project name from target path (simplification from previous version)
	// NOTE: If util.SanitizeModuleName were available and suitable for Spring's needs, it would be preferred.
	// We'll rely on a basic filepath.Base here as the Spring Initializr API uses this name directly.
	projectName := filepath.Base(targetPath)

	// URL-encode project name for query parameters
	encodedName := url.QueryEscape(projectName)

	// Sanitize package name: remove spaces and hyphens for Java package conventions
	sanitizedPackage := strings.ReplaceAll(projectName, "-", "")
	sanitizedPackage = strings.ReplaceAll(sanitizedPackage, " ", "")

	// Construct Spring Initializr URL
	zipURL := fmt.Sprintf(
		"https://start.spring.io/starter.zip?type=maven-project&language=java&groupId=com.example&artifactId=%s&name=%s&packageName=com.example.%s&packaging=jar&javaVersion=17&dependencies=web,lombok,actuator",
		encodedName, encodedName, sanitizedPackage,
	)

	const zipFile = "spring-init.zip"

	// All subsequent tasks will run inside the target path.

	// Task 1: Download Spring Boot starter zip
	// Download happens inside the target directory (RunInTarget: true)
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "curl",
		Args:        []string{"-f", "-sS", "-o", zipFile, zipURL},
		Desc:        fmt.Sprintf("Download Spring Boot starter for '%s'", projectName),
		TaskPrio:    100,
		RunInTarget: true, // All tasks run in targetPath for consistency
	})

	// Task 2: Extract the zip.
	// Since we are already inside targetPath (RunInTarget: true),
	// we extract in place and use the -j flag.
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "unzip",
		Args: []string{
			"-q",
			"-o",
			"-j", // Junk paths
			zipFile,
		},
		Desc:        "Extract project files (junking internal paths)",
		TaskPrio:    101,
		RunInTarget: true,
	})

	// Task 3: Remove the zip file after extraction. Runs in targetPath.
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "rm",
		Args:        []string{zipFile},
		Desc:        "Cleanup initialization artifacts",
		TaskPrio:    102,
		RunInTarget: true,
	})

	// Task 4: Make Maven wrapper executable.
	// Runs in targetPath, 'mvnw' is local.
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "chmod",
		Args:        []string{"+x", "mvnw"}, // Simplified path: just "mvnw"
		Desc:        "Make Maven wrapper executable",
		TaskPrio:    103,
		RunInTarget: true,
	})

	return plan, nil
}
