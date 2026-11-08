# .golangci.yml - Standardized Go Linter Configuration

# golangci-lint v2 configuration schema.
version: "2"

run:
  # Default timeout for linter
  timeout: 5m
  # Include test files
  tests: true

linters:
  # Start from no linters and enable only the set below.
  default: none
  enable:
    ## Core & Bugs
    - errcheck      # Checking for unchecked errors
    - govet         # Reports suspicious constructs
    - staticcheck   # Go static analysis, many checks
    - unused        # Checks for unused constants, variables, functions and types
    - bodyclose     # Checks whether HTTP response body is closed successfully
    - noctx         # Finds sending HTTP requests without context.Context

    ## Style & Formatting
    - revive        # Fast, configurable, extensible, flexible, and beautiful linter for Go
    - whitespace    # Tool for detection of leading and trailing whitespace
    - ineffassign   # Detects when assignments to existing variables are not used
    - misspell      # Finds commonly misspelled English words in comments

    ## Security
    - gosec         # Inspects source code for security problems

issues:
  max-issues-per-linter: 0
  max-same-issues: 0

output:
  formats:
    text:
      path: stdout
      print-issued-lines: true
      print-linter-name: true
