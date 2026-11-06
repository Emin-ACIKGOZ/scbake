version: 2
updates:
{{ range .Projects }}
  {{ if eq .Language "go" }}
  - package-ecosystem: "gomod"
    directory: {{ .Path | quote }}
    schedule:
      interval: "weekly"
  {{ end }}
  {{ if eq .Language "svelte" }}
  - package-ecosystem: "npm"
    directory: {{ .Path | quote }}
    schedule:
      interval: "weekly"
  {{ end }}
  {{ if eq .Language "spring" }}
  - package-ecosystem: "maven"
    directory: {{ .Path | quote }}
    schedule:
      interval: "weekly"
  {{ end }}
{{ end }}
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
