package main

import (
	"flag"
	"net/http"
	"net/http/httputil"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"

	"github.com/pgrok/pgrok/internal/sshd"
)

func main() {
	listenAddr := flag.String("listen-addr", "0.0.0.0:3000", "the web server listen address")
	sshdPort := flag.Int("sshd-port", 2222, "the port number of the SSH server")
	flag.Parse()

	log.SetLevel(log.DebugLevel)

	go func() {
		err := sshd.Start(
			log.New(
				log.WithTimestamp(),
				log.WithTimeFormat("2006-01-02 15:04:05"),
				log.WithPrefix("sshd"),
				log.WithLevel(log.DebugLevel),
			),
			*sshdPort)
		if err != nil {
			log.Fatal("Failed to start SSH server", "error", err)
		}
	}()

	reverseProxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// TODO get port based on host
			req.URL.Scheme = "http"
			req.URL.Host = "localhost:7777"
		},
	}
	f := flamego.New()
	f.Any("/{**}", reverseProxy.ServeHTTP)

	log.Info("Web server listening on "+*listenAddr,
		"env", flamego.Env(),
	)
	err := http.ListenAndServe(*listenAddr, f)
	if err != nil {
		log.Fatal("Failed to start web server", "error", err)
	}
}
