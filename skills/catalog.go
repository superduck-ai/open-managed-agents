package catalog

import "embed"

//go:embed catalog.json xlsx pptx pdf docx
var Files embed.FS
