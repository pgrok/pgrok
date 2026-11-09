package main

import (
	"net/http"
	"strings"

	"github.com/flamego/flamego"
)

func main() {
	f := flamego.Classic()
	f.Get("/echo", func(c flamego.Context) string {
		return c.Query("q")
	})
	f.Get("/echo/headers", func(r *http.Request) string {
		var b strings.Builder
		_ = r.Header.Write(&b)
		return b.String()
	})
	f.Run(8080)
}
