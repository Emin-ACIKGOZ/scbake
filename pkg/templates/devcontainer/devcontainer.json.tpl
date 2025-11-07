{
  "name": "scbake Dev Container",
  "dockerFile": "Dockerfile",
  "remoteUser": "vscode",
  "customizations": {
    "vscode": {
      "settings": {
        "terminal.integrated.defaultProfile.linux": "bash",
        "files.trimTrailingWhitespace": true,
        "editor.tabSize": 4
      },
      "extensions": [
        "editorconfig.editorconfig"
        {{ $root := . }}
        {{ range .Projects }}
          {{ if eq .Language "go" }}
          , "golang.go"
          {{ end }}
          {{ if eq .Language "svelte" }}
          , "svelte.svelte-vscode"
          , "dbaeumer.vscode-eslint"
          {{ end }}
          {{ if eq .Language "spring" }}
          , "vscjava.vscode-java-pack"
          {{ end }}
        {{ end }}
      ]
    }
  },
  "postCreateCommand": "npm install -g npm && /bin/bash"
}
