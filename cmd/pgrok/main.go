package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "pgrok.yml", "the path to the config file")
	debug := flag.Bool("debug", false, "whether to enable debug mode")
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}
	log.SetTimeFormat("2006-01-02 15:04:05")

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", *configPath,
			"error", err.Error(),
		)
	}

	f := flamego.New()
	if config.DynamicForwards != "" {
		rules := strings.Split(config.DynamicForwards, "\n")
		for _, rule := range rules {
			if rule == "" {
				continue
			}

			fields := strings.Fields(rule)
			if len(fields) != 2 {
				log.Debug("Skipped invalid dynamic forward rule", "rule", rule)
				continue
			}
			routePath := fmt.Sprintf("/{*: %s.+/}/{**}", fields[0])
			forward, err := url.Parse(fields[1])
			if err != nil {
				log.Fatal("Failed to parse the forward address",
					"rule", rule,
					"error", err.Error(),
				)
			}
			f.Any(routePath, httputil.NewSingleHostReverseProxy(forward).ServeHTTP)
			log.Debug("Dynamic forward rule added", "path", fields[0], "forwardTo", forward.String())
		}
	}

	defaultForward, err := url.Parse(config.ForwardAddr)
	if err != nil {
		log.Fatal("Failed to parse default forward address", "error", err.Error())
	}
	f.Any("/{**}", httputil.NewSingleHostReverseProxy(defaultForward).ServeHTTP)

	s := httptest.NewServer(f)
	log.Debug("Capture server is running on", "url", s.URL)

	surl, _ := url.Parse(s.URL)
	backoff := 3 * time.Second
	for {
		err := tryConnect(config.RemoteAddr, surl.Host, config.Token)
		if err != nil {
			log.Error("Failed to connect to server, will reconnect in "+backoff.String(), "error", err.Error())
			if strings.Contains(err.Error(), "no supported methods remain") {
				log.Fatal("Please double check your token and try again")
			}
		}
		time.Sleep(3 * time.Second)
	}
}

type Config struct {
	RemoteAddr      string `yaml:"remote_addr"`
	ForwardAddr     string `yaml:"forward_addr"`
	Token           string `yaml:"token"`
	DynamicForwards string `yaml:"dynamic_forwards"`
}

func loadConfig(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config
	err = yaml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}
	return &config, nil
}

func tryConnect(remoteAddr, forwardAddr, token string) error {
	client, err := ssh.Dial(
		"tcp",
		remoteAddr,
		&ssh.ClientConfig{
			User: "pgrok",
			Auth: []ssh.AuthMethod{
				ssh.Password(token),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		return errors.Wrap(err, "dial remote server")
	}

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("Failed to open port on remote connection", "error", err)
	}
	defer func() { _ = remoteListener.Close() }()
	log.Info("Tunneling connection established", "remote", remoteAddr)

	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			return errors.Wrap(err, "accept connection from server")
		}

		forward, err := net.Dial("tcp", forwardAddr)
		if err != nil {
			_ = remote.Close()
			log.Error("Failed to dial local forward", "error", err)
			continue
		}
		log.Debug("Forwarding connection", "remote", remote.RemoteAddr())

		go func() {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				log.Debug("Forwarding connection closed", "remote", remote.RemoteAddr())
			}()

			started := time.Now()
			var reqBuf, respBuf bytes.Buffer

			ctx, done := context.WithCancel(context.Background())
			go func() {
				w := io.MultiWriter(&headerWriter{W: &reqBuf}, forward)
				_, _ = io.Copy(w, remote)
				done()
			}()
			go func() {
				w := io.MultiWriter(&headerWriter{W: &respBuf}, remote)
				_, _ = io.Copy(w, forward)
				done()
			}()
			<-ctx.Done()

			req, err := http.ReadRequest(bufio.NewReader(&reqBuf))
			if err != nil {
				log.Error("Failed to read request",
					"remote", remote.RemoteAddr(),
					"error", err,
				)
				return
			}
			resp, err := http.ReadResponse(bufio.NewReader(&respBuf), req)
			if err != nil {
				log.Error("Failed to read response",
					"remote", remote.RemoteAddr(),
					"error", err,
				)
				return
			}
			log.Info("Forwarded request",
				"remote", remote.RemoteAddr(),
				"path", req.URL.Path,
				"status", resp.StatusCode,
				"duration", time.Since(started),
			)
		}()
	}
}

// A headerWriter writes to W until the request/response header has been
// written.
type headerWriter struct {
	W    io.Writer
	done bool
}

func (w *headerWriter) Write(p []byte) (int, error) {
	if w.done {
		return len(p), nil
	}

	r := false
	lines := 0
	for i := range p {
		if p[i] == '\r' {
			r = true
		} else if r && p[i] == '\n' {
			lines++
		} else {
			r = false
			lines = 0
		}

		if lines >= 2 {
			w.done = true
			break
		}
	}
	return w.W.Write(p)
}
