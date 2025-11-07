package spring

import (
	"fmt"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

type Handler struct{}

func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Determine project name (e.g., "backend")
	projectName := filepath.Base(targetPath)
	if projectName == "." || projectName == "/" {
		abs, _ := filepath.Abs(targetPath)
		projectName = filepath.Base(abs)
	}

	// Construct the standard Spring Initializr URL.
	// We use standard, opinionated defaults for v1.
	// dependencies: web (REST API standard), lombok (boilerplate reducer), actuator (health checks)
	url := fmt.Sprintf(
		"https://start.spring.io/starter.zip?type=maven-project&language=java&bootVersion=3.2.3&baseDir=.&groupId=com.example&artifactId=%s&name=%s&packageName=com.example.%s&packaging=jar&javaVersion=17&dependencies=web,lombok,actuator",
		projectName, projectName, projectName,
	)

	zipFile := "spring-init.zip"

	// Task 1: Download the zip
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "curl",
		Args:        []string{"-f", "-sS", "-o", zipFile, url}, // -f fails on HTTP errors, -sS is silent but shows errors
		Desc:        fmt.Sprintf("Download Spring Boot starter for '%s'", projectName),
		TaskPrio:    100,
		RunInTarget: true,
	})

	// Task 2: Unzip it
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "unzip",
		Args:        []string{"-q", "-o", zipFile}, // -q quiet, -o overwrite (we rely on scbake safety checks instead)
		Desc:        "Extract project files",
		TaskPrio:    101,
		RunInTarget: true,
	})

	// Task 3: Cleanup zip
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "rm",
		Args:        []string{zipFile},
		Desc:        "Cleanup initialization artifacts",
		TaskPrio:    102,
		RunInTarget: true,
	})

	// Task 4: Make mvnw executable (sometimes lost in zipping/unzipping depending on OS)
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "chmod",
		Args:        []string{"+x", "mvnw"},
		Desc:        "Make Maven wrapper executable",
		TaskPrio:    103,
		RunInTarget: true,
	})

	return plan, nil
}
