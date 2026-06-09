version: 2
updates:
{{ range .Projects }}
  {{ if eq .Language "go" }}
  - package-ecosystem: "gomod"
    directory: "{{ .Path }}"
    schedule:
      interval: "weekly"
  {{ end }}
  {{ if eq .Language "svelte" }}
  - package-ecosystem: "npm"
    directory: "{{ .Path }}"
    schedule:
      interval: "weekly"
  {{ end }}
{{ end }}
