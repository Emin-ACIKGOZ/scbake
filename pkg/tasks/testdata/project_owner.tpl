owner={{ if .Project }}{{ .Project.Name }}{{ else }}none{{ end }} first={{ if .Projects }}{{ (index .Projects 0).Name }}{{ else }}none{{ end }}
