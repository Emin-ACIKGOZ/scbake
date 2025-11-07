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

	// Construct URL
	url := fmt.Sprintf(
		"https://start.spring.io/starter.zip?type=maven-project&language=java&groupId=com.example&artifactId=%s&name=%s&packageName=com.example.%s&packaging=jar&javaVersion=17&dependencies=web,lombok,actuator",
		projectName, projectName, projectName,
	)

	zipFile := "spring-init.zip"

	// Task 1: Download the zip
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "curl",
		Args:        []string{"-f", "-sS", "-o", zipFile, url},
		Desc:        fmt.Sprintf("Download Spring Boot starter for '%s'", projectName),
		TaskPrio:    100,
		RunInTarget: false,
	})

	// Task 2: Unzip it
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

	// Task 3: Cleanup zip
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "rm",
		Args:        []string{zipFile},
		Desc:        "Cleanup initialization artifacts",
		TaskPrio:    102,
		RunInTarget: false,
	})

	// Task 4: Make mvnw executable
	plan = append(plan, &tasks.ExecCommandTask{
		Cmd:         "chmod",
		Args:        []string{"+x", "mvnw"},
		Desc:        "Make Maven wrapper executable",
		TaskPrio:    103,
		RunInTarget: true,
	})

	return plan, nil
}
