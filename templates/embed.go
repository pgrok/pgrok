package templates

import (
	"embed"
)

//go:embed *.tmpl
var Templates embed.FS
