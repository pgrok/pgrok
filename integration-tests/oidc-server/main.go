package main

import (
	"flag"

	"github.com/flamego/flamego"
)

func main() {
	port := flag.Int("port", 9833, "The port to listen on")
	flag.Parse()

	f := flamego.New()
	f.Get("/.well-known/openid-configuration", func() string {
		// TODO
		return "pong"
	})
	f.Run(*port)
}
