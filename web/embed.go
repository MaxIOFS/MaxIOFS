package web

import (
	"embed"
	"io/fs"
)

// Embed the frontend static files (all files including those starting with . or _)
//
//go:embed all:frontend/dist
var FrontendAssets embed.FS

// GetFrontendFS returns the embedded filesystem with the correct root
func GetFrontendFS() (fs.FS, error) {
	return fs.Sub(FrontendAssets, "frontend/dist")
}
