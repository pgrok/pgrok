package main

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"github.com/sourcegraph/run"
	"go.bobheadxi.dev/streamline/streamexec"
)

func TestMain(m *testing.M) {
	long := flag.Bool("long", false, "Enable the integration tests to run. Required flag, otherwise tests are skipped.")
	flag.Parse()

	if !*long {
		log.Print("Skipping integration tests since -long is not specified.")
		return
	}

	ctx := context.Background()

	shutdownOIDCServer, err := setupOIDCServer(ctx)
	if err != nil {
		log.Print("Failed to setup OIDC server", "error", err)
		return
	}
	defer func() {
		err := shutdownOIDCServer()
		if err != nil {
			log.Print("Failed to shutdown OIDC server", "error", err)
		}
	}()
	shutdownPgrokd, err := setupPgrokd(ctx)
	if err != nil {
		log.Print("Failed to setup pgrokd", "error", err)
		return
	}
	defer func() {
		err = shutdownPgrokd()
		if err != nil {
			log.Print("Failed to shutdown pgrokd", "error", err)
		}
	}()

	// TODO: authenticate the test user

	m.Run()
}

func setupOIDCServer(ctx context.Context) (shutdown func() error, _ error) {
	cmd := exec.Command("go", "run", "./oidc-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start OIDC server
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
	err := run.Cmd(ctx, "curl", "http://localhost:9833/.well-known/openid-configuration").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "probe OIDC server liveness")
	}
	log.Print("OIDC server started")

	return func() error { return kill(cmd.Process.Pid) }, nil
}

func setupPgrokd(ctx context.Context) (shutdown func() error, _ error) {
	cmd := exec.Command("go", "run", "../cmd/pgrokd")
	cmd.Env = append(cmd.Environ(), "FLAMEGO_ENV="+string(flamego.EnvTypeProd))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start pgrokd
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

	// Make sure the web server is live
	time.Sleep(3 * time.Second)
	err := run.Cmd(ctx, "curl", "http://localhost:3320/signin").Run().Wait()
	if err != nil {
		return nil, errors.Wrap(err, "probe web server liveness")
	}
	log.Print("web server started")

	return func() error { return kill(cmd.Process.Pid) }, nil
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

func TestHTTP(t *testing.T) {
	// Set up test HTTP server
	// Test pgrok with HTTP
}

func TestTCP(t *testing.T) {
	// Set up test TCP server
	// Test pgrok with TCP
}
