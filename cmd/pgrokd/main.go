package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"

	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/sshd"
)

func main() {
	listenAddr := flag.String("listen-addr", "0.0.0.0:3000", "the web server listen address")
	sshdPort := flag.Int("sshd-port", 2222, "the port number of the SSH server")
	flag.Parse()

	log.SetLevel(log.DebugLevel)

	proxies := reverseproxy.NewCluster()
	go func() {
		err := sshd.Start(
			log.New(
				log.WithTimestamp(),
				log.WithTimeFormat("2006-01-02 15:04:05"),
				log.WithPrefix("sshd"),
				log.WithLevel(log.DebugLevel),
			),
			*sshdPort,
			func(host, forward string) { proxies.Set(host, forward) },
			func(host string) { proxies.Remove(host) },
		)
		if err != nil {
			log.Fatal("Failed to start SSH server", "error", err)
		}
	}()

	f := flamego.New()
	f.Any("/{**}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Host:", r.Host)
		proxy, ok := proxies.Get(r.Host)
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("No reverse proxy is available for the host: " + r.Host))
			return
		}
		proxy.ServeHTTP(w, r)
	})

	log.Info("Web server listening on "+*listenAddr,
		"env", flamego.Env(),
	)
	err := http.ListenAndServe(*listenAddr, f)
	if err != nil {
		log.Fatal("Failed to start web server", "error", err)
	}
}
