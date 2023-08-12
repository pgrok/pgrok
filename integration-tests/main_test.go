//go:build !windows

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"github.com/sourcegraph/run"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bobheadxi.dev/streamline/streamexec"
	"golang.org/x/net/publicsuffix"
)

var (
	token string
	url   string
)

func TestMain(m *testing.M) {
	long := flag.Bool("long", false, "Enable the integration tests to run. Required flag, otherwise tests are skipped.")
	flag.Parse()

	if !*long {
		log.Print("Skipping integration tests since -long is not specified.")
		return
	}

	code := 0
	defer func() {
		if code != 0 {
			os.Exit(code)
		}
	}()

	ctx := context.Background()

	shutdownOIDCServer, err := setupOIDCServer(ctx)
	if err != nil {
		code = 1
		log.Print("Failed to setup OIDC server", "error", err)
		return
	}
	defer func() {
		err = shutdownOIDCServer()
		if err != nil {
			log.Print("Failed to shutdown OIDC server", "error", err)
		}
	}()
	shutdownPgrokd, err := setupPgrokd(ctx)
	if err != nil {
		code = 1
		log.Print("Failed to setup pgrokd", "error", err)
		return
	}
	defer func() {
		err = shutdownPgrokd()
		if err != nil {
			log.Print("Failed to shutdown pgrokd", "error", err)
		}
	}()

	token, url, err = authenticateUser()
	if err != nil {
		code = 1
		log.Print("Failed to authenticate user", "error", err)
		return
	}
	log.Print("Authenticated user", "token", token, "url", url)

	code = m.Run()
}

func setupOIDCServer(ctx context.Context) (shutdown func() error, _ error) {
	err := run.Cmd(ctx, "go", "build", "-o", "../.bin/oidc-server", "./oidc-server").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "go build")
	}

	cmd := exec.Command("../.bin/oidc-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	go func() {
		stream, err := streamexec.Start(cmd)
		if err != nil {
			log.Print("Failed to start OIDC server", "error", err)
			return
		}
		err = stream.Stream(func(line string) {
			fmt.Println("[oidc-server]", line)
		})
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Print("Failed to stream OIDC server output", "error", err)
			return
		}
		log.Print("OIDC server exited")
	}()

	// Make sure the OIDC server is live
	time.Sleep(time.Second)
	err = run.Cmd(ctx, "curl", "http://localhost:9833/.well-known/openid-configuration").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "probe OIDC server liveness")
	}
	log.Print("OIDC server started")

	return func() error { return kill(cmd.Process.Pid) }, nil
}

func setupPgrokd(ctx context.Context) (shutdown func() error, _ error) {
	err := run.Cmd(ctx, "go", "build", "-o", "../.bin/pgrokd", "../cmd/pgrokd").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "go build")
	}

	cmd := exec.Command("../.bin/pgrokd")
	cmd.Env = append(cmd.Environ(), "FLAMEGO_ENV="+string(flamego.EnvTypeProd))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	go func() {
		stream, err := streamexec.Start(cmd)
		if err != nil {
			log.Print("Failed to start pgrokd", "error", err)
			return
		}
		err = stream.Stream(func(line string) {
			fmt.Println("[pgrokd]", line)
		})
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Print("Failed to stream pgrokd output", "error", err)
			return
		}
		log.Print("pgrokd exited")
	}()

	// Make sure the pgrokd web server is live
	time.Sleep(3 * time.Second)
	err = run.Cmd(ctx, "curl", "http://localhost:3320/signin").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "probe pgrokd web server liveness")
	}
	log.Print("pgrokd started")

	return func() error { return kill(cmd.Process.Pid) }, nil
}

func authenticateUser() (token, url string, _ error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return "", "", errors.Wrap(err, "new cookie jar")
	}
	client := &http.Client{Jar: jar}
	resp, err := client.Get("http://localhost:3320/-/oidc/auth")
	if err != nil {
		return "", "", errors.Wrap(err, "sign in")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", errors.Wrap(err, "read response body")
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", errors.Errorf("unexpected status code: %d - %s", resp.StatusCode, body)
	}
	log.Print("Got sign in page", "body", string(body))

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", errors.Wrap(err, "parse home page")
	}
	token = doc.Find("#token").Get(0).FirstChild.Data
	url = doc.Find("#url").Get(0).Attr[1].Val // href
	if token == "" || url == "" {
		return "", "", errors.New(`"token" or "url" not found`)
	}
	return token, url, nil
}

func kill(pid int) error {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return errors.Wrap(err, "get process group pid")
	}

	for i := 0; i < 10; i++ {
		err := syscall.Kill(-pgid, syscall.SIGKILL)
		if err != nil {
			if strings.Contains(err.Error(), "no such process") {
				return nil
			}
			return errors.Wrap(err, "kill")
		}

		time.Sleep(time.Second)
	}
	return errors.New("cannot kill the process after 10 tries")
}

func setupPgrok(ctx context.Context, protocol string) (endpoint string, shutdown func() error, _ error) {
	err := run.Cmd(ctx, "go", "build", "-o", "../.bin/pgrok", "../cmd/pgrok").Run().Wait()
	if err != nil {
		return "", nil, errors.Wrap(err, "go build")
	}

	args := []string{
		protocol,
		"--config", "pgrok.yml",
		"--token", token,
	}
	if protocol == "tcp" {
		args = append(args, "--forward-addr", "localhost:9833")
	}

	cmd := exec.Command("../.bin/pgrok", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	started := false
	ready := make(chan struct{})
	go func() {
		stream, err := streamexec.Start(cmd)
		if err != nil {
			log.Print("Failed to start pgrok", "error", err)
			return
		}
		err = stream.Stream(func(line string) {
			fmt.Printf("[pgrok-%s] %s\n", protocol, line)
			if !started && strings.Contains(line, "You're ready to go live") {
				started = true
				endpoint = line[strings.Index(line, "://")+3 : strings.Index(line, "!")]
				ready <- struct{}{}
			}
		})
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Print("Failed to stream pgrok output", "error", err)
			return
		}
		log.Printf("pgrok %s exited", protocol)
	}()

	// Make sure the pgrok is ready
	select {
	case <-ready:
		log.Print("pgrok started", "protocol", protocol, "endpoint", endpoint)
	case <-time.After(5 * time.Second):
		return "", nil, errors.New("pgrok failed to start after 5 seconds")
	}

	return endpoint, func() error { return kill(cmd.Process.Pid) }, nil
}

func setupEchoServer(ctx context.Context) (shutdown func() error, _ error) {
	err := run.Cmd(ctx, "go", "build", "-o", "../.bin/echo-server", "./echo-server").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "go build")
	}

	cmd := exec.Command("../.bin/echo-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	go func() {
		stream, err := streamexec.Start(cmd)
		if err != nil {
			log.Print("Failed to start echo server", "error", err)
			return
		}
		err = stream.Stream(func(line string) {
			fmt.Println("[echo-server]", line)
		})
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			log.Print("Failed to stream echo server output", "error", err)
			return
		}
		log.Print("echo server exited")
	}()

	// Make sure the server is live
	time.Sleep(time.Second)
	err = run.Cmd(ctx, "curl", "http://localhost:8080").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "probe echo server liveness")
	}
	log.Print("echo server started")

	return func() error { return kill(cmd.Process.Pid) }, nil
}

func TestHTTP(t *testing.T) {
	t.Run("pgrok", func(t *testing.T) {
		ctx := context.Background()

		shutdownEchoServer, err := setupEchoServer(ctx)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, shutdownEchoServer())
		}()

		_, shutdownPgrok, err := setupPgrok(ctx, "http")
		require.NoError(t, err)
		defer func() {
			require.NoError(t, shutdownPgrok())
		}()

		// Default forward
		body, err := run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("%s/.well-known/openid-configuration", url)).Run().String()
		require.NoError(t, err)
		assert.Contains(t, body, `"issuer": "http://localhost:9833",`)

		// Dynamic forward
		body, err = run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("%s/echo?q=chickendinner", url)).Run().String()
		require.NoError(t, err)
		assert.Contains(t, body, "chickendinner")
	})

	t.Run("ssh", func(t *testing.T) {
		// todo
	})
}

func TestTCP(t *testing.T) {
	ctx := context.Background()
	endpoint, shutdownPgrok, err := setupPgrok(ctx, "tcp")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, shutdownPgrok())
	}()

	body, err := run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("http://%s/.well-known/openid-configuration", endpoint)).Run().String()
	require.NoError(t, err)
	assert.Contains(t, body, `"issuer": "http://localhost:9833",`)
}
