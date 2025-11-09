// Package spring provides the task handler for initializing Spring Boot projects.
package spring

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// Handler implements the lang.Handler interface for Spring Boot projects.
type Handler struct{}

// GetTasks returns the execution plan for initializing a Spring Boot project at targetPath.
func (h *Handler) GetTasks(targetPath string) ([]types.Task, error) {
	var plan []types.Task

	// Initialize sequence for setup band
	dirSeq := types.NewPrioritySequence(types.PrioDirCreate, types.MaxDirCreate)
	langSeq := types.NewPrioritySequence(types.PrioLangSetup, types.MaxLangSetup)

	// Task 0: Ensure target directory exists (always needed)
	p, err := dirSeq.Next()
	if err != nil {
		return nil, err
	}
	plan = append(plan, &tasks.CreateDirTask{
		Path:     targetPath,
		Desc:     fmt.Sprintf("Create project directory '%s'", targetPath),
		TaskPrio: int(p), // Now 50
	})

	// Idempotency Check: Check for existence of pom.xml
	pomXMLPath := filepath.Join(targetPath, "pom.xml")
	_, checkErr := os.Stat(pomXMLPath)

	if os.IsNotExist(checkErr) {
		// --- Path 1: pom.xml does NOT exist (Initialization) ---

		// Determine project name from target path.
		projectName := filepath.Base(targetPath)

		// URL-encode project name for query parameters.
		encodedName := url.QueryEscape(projectName)

		// Sanitize package name for Java conventions.
		sanitizedPackage := strings.ReplaceAll(projectName, "-", "")
		sanitizedPackage = strings.ReplaceAll(sanitizedPackage, " ", "")

		// Construct Spring Initializr URL.
		zipURL := fmt.Sprintf(
			"https://start.spring.io/starter.zip?type=maven-project&language=java&groupId=com.example&artifactId=%s&name=%s&packageName=com.example.%s&packaging=jar&javaVersion=17&dependencies=web,lombok,actuator",
			encodedName, encodedName, sanitizedPackage,
		)

		const zipFile = "spring-init.zip"

		// Task 1: Download Spring Boot starter zip
		p, err := langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "curl",
			Args:        []string{"-f", "-sS", "-o", zipFile, zipURL},
			Desc:        fmt.Sprintf("Download Spring Boot starter for '%s'", projectName),
			TaskPrio:    int(p), // Now 100
			RunInTarget: true,
		})

		// Task 2: Extract the zip, preserving directory structure
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd: "unzip",
			Args: []string{
				"-q",
				"-o",
				zipFile,
			},
			Desc:        "Extract project files",
			TaskPrio:    int(p), // Now 101
			RunInTarget: true,
		})

		// Task 3: Remove the zip file after extraction.
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "rm",
			Args:        []string{zipFile},
			Desc:        "Cleanup initialization artifacts",
			TaskPrio:    int(p), // Now 102
			RunInTarget: true,
		})

		// Task 4: Make Maven wrapper executable
		p, err = langSeq.Next()
		if err != nil {
			return nil, err
		}
		plan = append(plan, &tasks.ExecCommandTask{
			Cmd:         "chmod",
			Args:        []string{"+x", "mvnw"},
			Desc:        "Make Maven wrapper executable",
			TaskPrio:    int(p), // Now 103
			RunInTarget: true,
		})
	} else if checkErr != nil {
		// Path 2 (pom.xml *does* exist, checkErr == nil) now falls through to return plan, nil.
		// If no initialization tasks were run, plan contains only the CreateDirTask.

		// --- Path 3: Some other error ---
		return nil, fmt.Errorf("could not check for pom.xml: %w", checkErr)
	}

	return plan, nil
}
