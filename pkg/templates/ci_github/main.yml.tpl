name: CI Build

on:
  push:
    branches: [ "main", "master" ]
  pull_request:
    branches: [ "main", "master" ]

jobs:
  build:
    runs-on: ubuntu-latest

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
                go-version: '1.21'
            {{ $hasGo = true }}
          {{ end }}
        {{ end }}
        
        {{ if eq .Language "svelte" }}
          {{ if not $hasSvelte }}
            - name: Setup Node.js Environment
              uses: actions/setup-node@v4
              with:
                node-version: '20'
            {{ $hasSvelte = true }}
          {{ end }}
        {{ end }}
        
        {{ if eq .Language "spring" }}
          {{ if not $hasSpring }}
            - name: Setup Java Environment
              uses: actions/setup-java@v4
              with:
                distribution: 'temurin'
                java-version: '17'
          {{ $hasSpring = true }}
          {{ end }}
        {{ end }}
      {{ end }}

      # --- Conditional Build Steps per Project ---
      - name: Build and Test All Projects
        run: |
          echo "Starting monorepo build..."
          
          {{ range .Projects }}
            echo "--- Building {{ .Name }} ({{ .Language }}) ---"
            
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
            # Build Spring Project using Maven Wrapper
            cd {{ .Path }}
            ./mvnw clean package -DskipTests
            cd -
            {{ end }}
          {{ end }}