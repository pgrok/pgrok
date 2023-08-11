package main

import (
	"github.com/flamego/flamego"
)

func main() {
	f := flamego.New()
	f.Get("/", func(c flamego.Context) string {
		return c.Query("echo")
	})
	f.Run()
}
