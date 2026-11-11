package httpapi

import "embed"

//go:embed static/*.js static/*.css
var DocumentationAssets embed.FS
