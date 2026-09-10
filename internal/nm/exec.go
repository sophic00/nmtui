package nm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	scanTimeout    = 20 * time.Second
	connectTimeout = 60 * time.Second
	connectWait    = "45"
)

// Runner executes nmcli. Injecting it keeps the nm package testable without
// spawning processes.
type Runner interface {
	Run(ctx context.Context, timeout time.Duration, stdin string, args ...string) (string, error)
}

// ExecRunner runs the real nmcli binary with a hard timeout and kills the
// whole process group when the context is cancelled.
type ExecRunner struct {
	// Bin overrides the executable to run; empty means "nmcli".
	Bin string
}

func (r ExecRunner) Run(ctx context.Context, timeout time.Duration, stdin string, args ...string) (string, error) {
	bin := r.Bin
	if bin == "" {
		bin = "nmcli"
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LC_MESSAGES=C")
	// Run nmcli in its own process group so cancellation also kills any
	// helpers it spawned instead of leaving them behind.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", fmt.Errorf("nmcli timed out after %s", timeout)
		case errors.Is(ctx.Err(), context.Canceled):
			return "", context.Canceled
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if idx := strings.Index(msg, "Error:"); idx != -1 {
			msg = strings.TrimSpace(msg[idx:])
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// Client issues nmcli commands through a Runner.
type Client struct {
	runner Runner
}

func NewClient() *Client {
	return &Client{runner: ExecRunner{}}
}

// NewClientWithRunner builds a Client with a custom Runner, for tests.
func NewClientWithRunner(r Runner) *Client {
	return &Client{runner: r}
}

func (c *Client) runnerOrDefault() Runner {
	if c != nil && c.runner != nil {
		return c.runner
	}
	return ExecRunner{}
}

func (c *Client) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return c.runnerOrDefault().Run(ctx, timeout, "", args...)
}

func (c *Client) runWithStdin(ctx context.Context, timeout time.Duration, stdin string, args ...string) (string, error) {
	return c.runnerOrDefault().Run(ctx, timeout, stdin, args...)
}
