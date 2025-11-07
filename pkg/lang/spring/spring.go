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

	// Determine project name from target path
	projectName := filepath.Base(targetPath)
	if projectName == "." || projectName == "/" {
		abs, _ := filepath.Abs(targetPath)
		projectName = filepath.Base(abs)
	}

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

	// Task 1: Download Spring Boot starter zip
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "curl",
		Args:        []string{"-f", "-sS", "-o", zipFile, zipURL},
		Desc:        fmt.Sprintf("Download Spring Boot starter for '%s'", projectName),
		TaskPrio:    100,
		RunInTarget: false,
	})

	// Task 2: Extract the zip into the target directory
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd: "unzip",
		Args: []string{
			"-q",
			"-o",
			zipFile,
			"-d", targetPath, // Extract into the target directory
		},
		Desc:        "Extract project files",
		TaskPrio:    101,
		RunInTarget: false,
	})

	// Task 3: Remove the zip file after extraction
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "rm",
		Args:        []string{zipFile},
		Desc:        "Cleanup initialization artifacts",
		TaskPrio:    102,
		RunInTarget: false,
	})

	// Task 4: Make Maven wrapper executable if present
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "chmod",
		Args:        []string{"+x", "mvnw"},
		Desc:        "Make Maven wrapper executable",
		TaskPrio:    103,
		RunInTarget: true,
	})

	return plan, nil
}
