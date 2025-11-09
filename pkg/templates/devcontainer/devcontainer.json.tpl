{{- /* 1. Scan manifest once to determine required languages */ -}}
{{- $hasGo := false -}}
{{- $hasNode := false -}}
{{- $hasJava := false -}}
{{- range .Projects -}}
    {{- if eq .Language "go" }}{{ $hasGo = true }}{{ end -}}
    {{- if eq .Language "svelte" }}{{ $hasNode = true }}{{ end -}}
    {{- if eq .Language "spring" }}{{ $hasJava = true }}{{ end -}}
{{- end -}}
{
  "name": "scbake Dev Container",
  "dockerFile": "Dockerfile",
  "remoteUser": "vscode",
  "features": {
    "ghcr.io/devcontainers/features/common-utils:2": {
      "installZsh": "false",
      "username": "vscode",
      "upgradePackages": "true"
    }
    {{- if $hasGo -}}
    ,
    "ghcr.io/devcontainers/features/go:1": {
      "version": "latest"
    }
    {{- end -}}
    {{- if $hasNode -}}
    ,
    "ghcr.io/devcontainers/features/node:1": {
      "version": "lts"
    }
    {{- end -}}
    {{- if $hasJava -}}
    ,
    "ghcr.io/devcontainers/features/java:1": {
      "version": "17",
      "installMaven": "true",
      "installGradle": "false"
    }
    {{- end -}}
  },
  "customizations": {
    "vscode": {
      "settings": {
        "terminal.integrated.defaultProfile.linux": "bash",
        "files.trimTrailingWhitespace": true,
        "editor.tabSize": 4
      },
      "extensions": [
        "editorconfig.editorconfig"
        {{- if $hasGo -}}
        , "golang.go"
        {{- end -}}
        {{- if $hasNode -}}
        , "svelte.svelte-vscode"
        , "dbaeumer.vscode-eslint"
        {{- end -}}
        {{- if $hasJava -}}
        , "vscjava.vscode-java-pack"
        , "vscjava.vscode-maven"
        {{- end -}}
      ]
    }
  },
  "postCreateCommand": "echo 'Dev Container successfully initialized!'"
}
