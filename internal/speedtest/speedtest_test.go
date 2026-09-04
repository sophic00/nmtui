package speedtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testServer(downBytes int64, downChunkDelay time.Duration) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/__down", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Small fixed body; workers re-request until the window expires,
		// which exercises the re-request loop deterministically.
		const chunk = 32 * 1024
		buf := make([]byte, chunk)
		var written int64
		for written < downBytes {
			n := int64(len(buf))
			if written+n > downBytes {
				n = downBytes - written
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			written += n
			if downChunkDelay > 0 {
				time.Sleep(downChunkDelay)
			}
		}
	})
	mux.HandleFunc("/__up", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

func TestMbps(t *testing.T) {
	// 10MB over 10s = 8 Mbps.
	if got := Mbps(10_000_000, 10*time.Second); got < 7.99 || got > 8.01 {
		t.Errorf("Mbps = %v, want ~8", got)
	}
	if got := Mbps(0, 10*time.Second); got != 0 {
		t.Errorf("Mbps(0) = %v, want 0", got)
	}
	if got := Mbps(1000, 0); got != 0 {
		t.Errorf("Mbps with zero elapsed = %v, want 0", got)
	}
}

func TestFormatMbps(t *testing.T) {
	for _, tc := range []struct{ in float64 }{{0}, {8.5}, {99.9}, {342.1}, {1500}} {
		if got := FormatMbps(tc.in); got == "" {
			t.Errorf("FormatMbps(%v) empty", tc.in)
		}
	}
	if got := FormatMbps(1500); got != "1.50 Gbps" {
		t.Errorf("FormatMbps(1500) = %q, want Gbps rendering", got)
	}
}

func TestRunAgainstLocalServer(t *testing.T) {
	srv := testServer(256*1024, 0)
	defer srv.Close()

	cfg := Config{
		DownloadFor: 1500 * time.Millisecond,
		UploadFor:   1500 * time.Millisecond,
		Streams:     2,
		BaseURL:     srv.URL,
		PingSamples: 2,
		Client:      srv.Client(),
	}
	var progressCalls atomic.Int64
	res, err := Run(context.Background(), cfg, func(p Progress) {
		progressCalls.Add(1)
		if p.Phase != PhaseDownload && p.Phase != PhaseUpload {
			t.Errorf("unexpected progress phase %q", p.Phase)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.DownloadMbps <= 0 {
		t.Errorf("DownloadMbps = %v, want > 0", res.DownloadMbps)
	}
	if res.UploadMbps <= 0 {
		t.Errorf("UploadMbps = %v, want > 0", res.UploadMbps)
	}
	if res.LatencyMs <= 0 {
		t.Errorf("LatencyMs = %v, want > 0", res.LatencyMs)
	}
	if progressCalls.Load() == 0 {
		t.Error("expected progress callbacks during run")
	}
}

func TestRunCancel(t *testing.T) {
	srv := testServer(64*1024*1024, 5*time.Millisecond)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	cfg := Config{
		DownloadFor: 30 * time.Second,
		UploadFor:   30 * time.Second,
		Streams:     2,
		BaseURL:     srv.URL,
		PingSamples: 2,
		Client:      srv.Client(),
	}
	start := time.Now()
	_, err := Run(ctx, cfg, nil)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("cancel took too long: %v", elapsed)
	}
}

func TestRunBadServerFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "boom")
	}))
	defer srv.Close()

	cfg := Config{
		DownloadFor: time.Second,
		UploadFor:   time.Second,
		Streams:     1,
		BaseURL:     srv.URL,
		PingSamples: 1,
		Client:      srv.Client(),
	}
	start := time.Now()
	_, err := Run(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error from 500 ping, got nil")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("bad server did not fail fast: %v", elapsed)
	}
}
