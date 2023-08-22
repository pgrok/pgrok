package main

import (
	"flag"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
)

var version = "0.0.0+dev"

func main() {
	if strings.Contains(version, "+dev") {
		log.SetLevel(log.DebugLevel)
	} else {
		flamego.SetEnv(flamego.EnvTypeProd)
	}
	log.SetTimeFormat(time.DateTime)

	configPath := flag.String("config", "pgrokd.yml", "the path to the config file")
	flag.Parse()

	config, err := conf.Load(*configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", *configPath,
			"error", err.Error(),
		)
	}

	db, err := database.New(os.Stdout, config.Database)
	if err != nil {
		log.Fatal("Failed to connect to database", "error", err.Error())
	}

	proxies := reverseproxy.NewCluster()
	go startSSHServer(log.Default(), config.SSHD.Port, config.Proxy, db, proxies)
	go startProxyServer(log.Default(), config.Proxy.Port, proxies)
	go startWebServer(config, db)

	select {}
}
