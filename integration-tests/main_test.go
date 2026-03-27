//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/google/uuid"
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
	err := run.Cmd(ctx, "pnpm", "--dir ../pgrokd/web", "run", "build").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "pnpm run build")
	}

	err = run.Cmd(ctx, "go", "build", "-o", "../.bin/pgrokd", "../pgrokd/cli").Run().Wait()
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

	// Perform sign in
	resp, err := client.Get("http://localhost:3320/-/oidc/auth")
	if err != nil {
		return "", "", errors.Wrap(err, "sign in")
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", "", errors.Wrap(err, "read sign in response body")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", errors.Errorf("unexpected sign in status code: %d - %s", resp.StatusCode, body)
	}

	// Get user info
	resp, err = client.Get("http://localhost:3320/api/user-info")
	if err != nil {
		return "", "", errors.Wrap(err, "get user info")
	}
	body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", "", errors.Wrap(err, "read user info response body")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", errors.Errorf("unexpected user info status code: %d - %s", resp.StatusCode, body)
	}
	log.Print("Got user info", "body", string(body))

	var userInfo map[string]string
	err = json.Unmarshal(body, &userInfo)
	if err != nil {
		return "", "", errors.Wrap(err, "unmarshal user info")
	}

	token = userInfo["token"]
	url = userInfo["url"]
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

func setupPgrok(ctx context.Context, protocol string, port int) (endpoint string, shutdown func() error, _ error) {
	return setupPgrokWithSubdomain(ctx, protocol, port, "")
}

func setupPgrokWithSubdomain(ctx context.Context, protocol string, port int, uuid string) (endpoint string, shutdown func() error, _ error) {
	err := run.Cmd(ctx, "go", "build", "-o", "../.bin/pgrok", "../pgrok/cli").Run().Wait()
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
	if uuid != "" {
		args = append(args, "--uuid", uuid)
	}
	if port > 0 {
		args = append(args, strconv.Itoa(port))
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
	ctx := context.Background()

	shutdownEchoServer, err := setupEchoServer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownEchoServer()) })

	_, shutdownPgrok, err := setupPgrok(ctx, "http", 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownPgrok()) })

	// Default forward
	body, err := run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("%s/.well-known/openid-configuration", url)).Run().String()
	require.NoError(t, err)
	assert.Contains(t, body, `"issuer": "http://localhost:9833",`)

	// Dynamic forward
	body, err = run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("%s/echo?q=chickendinner", url)).Run().String()
	require.NoError(t, err)
	assert.Contains(t, body, "chickendinner")
}

func TestMultipleHTTP(t *testing.T) {
	ctx := context.Background()

	shutdownEchoServer, err := setupEchoServer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownEchoServer()) })

	endpoint1, shutdownPgrok1, err := setupPgrok(ctx, "http", 8001)
	require.NoError(t, err)
	require.Equal(t, "unknwon.localhost:3000", endpoint1)

	endpoint2, shutdownPgrok2, err := setupPgrok(ctx, "http", 8080)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownPgrok2()) })
	prefix, _, _ := strings.Cut(endpoint2, "-unknwon.localhost:3000")
	require.NoError(t, uuid.Validate(prefix))

	// Sanity-check on a request forward
	body, err := run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("%s/echo?q=chickendinner", url)).Run().String()
	require.NoError(t, err)
	require.Contains(t, body, "chickendinner")

	// The initial subdomain should be free to allocate again
	require.NoError(t, shutdownPgrok1())
	endpoint3, shutdownPgrok3, err := setupPgrok(ctx, "http", 8003)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownPgrok3()) })
	require.Equal(t, "unknwon.localhost:3000", endpoint3)
}

func TestCustomUuid(t *testing.T) {
	ctx := context.Background()

	shutdownEchoServer, err := setupEchoServer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, shutdownEchoServer()) })

	endpoint1, shutdownPgrok1, err := setupPgrokWithSubdomain(ctx, "http", 8001, "test")
	require.NoError(t, err)
	require.Equal(t, "test-unknwon.localhost:3000", endpoint1)
	require.NoError(t, shutdownPgrok1())
}

func TestTCP(t *testing.T) {
	ctx := context.Background()
	endpoint, shutdownPgrok, err := setupPgrok(ctx, "tcp", 0)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, shutdownPgrok())
	}()

	body, err := run.Cmd(ctx, "curl", "--silent", fmt.Sprintf("http://%s/.well-known/openid-configuration", endpoint)).Run().String()
	require.NoError(t, err)
	assert.Contains(t, body, `"issuer": "http://localhost:9833",`)
}
