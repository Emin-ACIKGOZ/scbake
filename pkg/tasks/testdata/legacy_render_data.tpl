projects={{ range .Projects }}{{ .Name }};{{ end }} license={{ index .Metadata "license" }} version={{ .SbakeVersion }}
