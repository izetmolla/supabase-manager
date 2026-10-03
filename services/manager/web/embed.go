// Package web embeds the manager UI built from frontend/apps/manager (vite outDir: services/manager/web/dist).
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
