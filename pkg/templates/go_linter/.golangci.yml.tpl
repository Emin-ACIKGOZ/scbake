# .golangci.yml - Standardized Go Linter Configuration

# Using modern config format (Version 2+)
Version: 2

run:
  # Default timeout for linter
  timeout: 5m
  # Include test files
  tests: true

output:
  format: colored-line-number
  print-issued-lines: true
  print-linter-name: true

linters:
  disable-all: true
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
  exclude-use-default: false
  max-issues-per-linter: 0
  max-same-issues: 0