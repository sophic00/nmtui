package nm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	scanTimeout    = 20 * time.Second
	connectTimeout = 60 * time.Second
	connectWait    = "45"
)

func runWithStdin(timeout time.Duration, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nmcli", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LC_MESSAGES=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
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

func run(timeout time.Duration, args ...string) (string, error) {
	return runWithStdin(timeout, "", args...)
}
