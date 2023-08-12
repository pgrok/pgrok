package main

import (
	"github.com/flamego/flamego"
)

func main() {
	f := flamego.Classic()
	f.Get("/echo", func(c flamego.Context) string {
		return c.Query("q")
	})
	f.Run(8080)
}
