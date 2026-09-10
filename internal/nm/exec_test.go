package nm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerTimeout(t *testing.T) {
	r := ExecRunner{Bin: "sleep"}
	_, err := r.Run(context.Background(), 50*time.Millisecond, "", "5")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout error = %q, want it to mention the timeout", err)
	}
}

func TestExecRunnerContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	r := ExecRunner{Bin: "sleep"}
	_, err := r.Run(ctx, time.Minute, "", "5")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancel error = %v, want context.Canceled", err)
	}
}

func TestExecRunnerStderrError(t *testing.T) {
	r := ExecRunner{Bin: "sh"}
	_, err := r.Run(context.Background(), time.Second, "", "-c", "echo 'Error: connection failed' >&2; exit 1")
	if err == nil || err.Error() != "Error: connection failed" {
		t.Errorf("stderr error = %v, want 'Error: connection failed'", err)
	}
}

func TestExecRunnerStdin(t *testing.T) {
	r := ExecRunner{Bin: "cat"}
	out, err := r.Run(context.Background(), time.Second, "secret\n")
	if err != nil {
		t.Fatalf("cat: %v", err)
	}
	if out != "secret\n" {
		t.Errorf("stdin passthrough = %q, want %q", out, "secret\n")
	}
}
