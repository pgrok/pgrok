//go:build prod

package main

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
)

//go:embed all:dist
var webAssets embed.FS

// setupWebAssets serves the embedded web app assets and routes all non-backend
// URLs to the SPA index so that page refreshes work.
func setupWebAssets(f *flamego.Flame) {
	webFS, err := fs.Sub(webAssets, "dist")
	if err != nil {
		log.Fatal("Failed to load embedded web assets", "error", err.Error())
		return
	}
	f.Use(flamego.Static(
		flamego.StaticOptions{
			FileSystem: http.FS(webFS),
		},
	))

	// Make sure the page refresh works
	indexFile, err := webAssets.Open("dist/index.html")
	if err != nil {
		log.Fatal(`Failed to open "dist/index.html"`, "error", err.Error())
		return
	}
	indexFileStat, err := indexFile.Stat()
	if err != nil {
		log.Fatal(`Failed to stat "dist/index.html"`, "error", err.Error())
		return
	}
	index, err := webAssets.ReadFile("dist/index.html")
	if err != nil {
		log.Fatal(`Failed to read "dist/index.html"`, "error", err.Error())
		return
	}
	indexReader := bytes.NewReader(index)
	f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", indexFileStat.ModTime(), indexReader)
	})
}
