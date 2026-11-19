name: CI Build

on:
  push:
    branches: [ "main", "master" ]
  pull_request:
    branches: [ "main", "master" ]

jobs:
  build:
    runs-on: {{ index .Metadata "runner_os" | default "ubuntu-latest" }}

    steps:
      - uses: actions/checkout@v4

      # --- Conditional Setup Steps based on Languages in Manifest ---
      {{ $root := . }}
      {{ $hasGo := false }}
      {{ $hasSvelte := false }}
      {{ $hasSpring := false }}

      {{ range .Projects }}
        {{ if eq .Language "go" }}
          {{ if not $hasGo }}
      - name: Setup Go Environment
        uses: actions/setup-go@v5
        with:
          {{ if index $root.Metadata "go_version" -}}
          go-version: {{ index $root.Metadata "go_version" | quote }}
          {{- else -}}
          go-version-file: {{ if eq .Path "." }}"go.mod"{{ else }}{{ printf "%s/go.mod" .Path | quote }}{{ end }}
          {{- end }}
            {{ $hasGo = true }}
          {{ end }}
        {{ end }}
        
        {{ if eq .Language "svelte" }}
          {{ if not $hasSvelte }}
      - name: Setup Node.js Environment
        uses: actions/setup-node@v4
        with:
          node-version: {{ index $root.Metadata "node_version" | default "20" | quote }}
            {{ $hasSvelte = true }}
          {{ end }}
        {{ end }}
        
        {{ if eq .Language "spring" }}
          {{ if not $hasSpring }}
      - name: Setup Java Environment
        uses: actions/setup-java@v4
        with:
          distribution: {{ index $root.Metadata "java_distribution" | default "temurin" | quote }}
          java-version: {{ index $root.Metadata "java_version" | default "17" | quote }}
          {{ $hasSpring = true }}
          {{ end }}
        {{ end }}
      {{ end }}

      # --- Conditional Build Steps per Project ---
      - name: Build and Test All Projects
        {{- if eq (index .Metadata "runner_os" | default "ubuntu-latest") "windows-latest" }}
        shell: bash
        {{- end }}
        run: |
          echo "Starting monorepo build..."
          
          {{ range .Projects }}
            echo {{ printf "--- Building %s (%s) ---" .Name .Language | quote }}
            
            {{ if eq .Language "go" }}
            # Build and Test Go Project
            cd {{ .Path }}
            go mod download
            go test ./...
            go build .
            cd -
            {{ end }}
            
            {{ if eq .Language "svelte" }}
            # Install and Build Svelte Project
            cd {{ .Path }}
            npm install --silent
            npm run build
            cd -
            {{ end }}
            
            {{ if eq .Language "spring" }}
            # Build and Test Spring Project using Maven Wrapper
            cd {{ .Path }}
            ./mvnw clean verify
            cd -
            {{ end }}
          {{ end }}