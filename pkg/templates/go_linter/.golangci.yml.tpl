# .golangci.yml - Standardized Go Linter Configuration

run:
  timeout: 5m

linters:
  enable:
    - dogsled
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - whitespace
  
issues:
  exclude-use-default: false
  max-per-linter: 0
  max-same-issues: 0