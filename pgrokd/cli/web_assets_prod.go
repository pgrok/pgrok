//go:build prod

package main

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
)

//go:embed all:dist
var webAssets embed.FS

func setupWebAssets(f *flamego.Flame) error {
	webFS, err := fs.Sub(webAssets, "dist")
	if err != nil {
		return errors.Wrap(err, "load embedded web assets")
	}
	f.Use(flamego.Static(
		flamego.StaticOptions{
			FileSystem: http.FS(webFS),
		},
	))

	// Make sure the page refresh works
	indexFile, err := webAssets.Open("dist/index.html")
	if err != nil {
		return errors.Wrap(err, `open "dist/index.html"`)
	}
	indexFileStat, err := indexFile.Stat()
	if err != nil {
		return errors.Wrap(err, `stat "dist/index.html"`)
	}
	index, err := webAssets.ReadFile("dist/index.html")
	if err != nil {
		return errors.Wrap(err, `read "dist/index.html"`)
	}
	indexReader := bytes.NewReader(index)
	f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", indexFileStat.ModTime(), indexReader)
	})
	return nil
}
