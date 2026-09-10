package speedtest

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	PhasePing     = "ping"
	PhaseDownload = "download"
	PhaseUpload   = "upload"
)

// downloadBytesPerRequest sizes a single GET. Cloudflare rejects large
// values (100MB+ returns 403), so stay at the 25MB size its own speed
// test uses; workers re-request on EOF anyway.
const downloadBytesPerRequest = 25_000_000

// uploadBytesPerRequest sizes a single POST body; workers re-POST on completion.
const uploadBytesPerRequest = 200_000_000

// progressTick is how often Run invokes report during download/upload.
const progressTick = 200 * time.Millisecond

type Config struct {
	DownloadFor time.Duration
	UploadFor   time.Duration
	Streams     int
	BaseURL     string
	// MaxBytes caps total transfer per phase as a safety valve
	// (e.g. gigabit link x 10s). <=0 means no cap.
	MaxBytes int64
	// PingSamples defaults to 5.
	PingSamples int
	// Client allows tests to inject a custom *http.Client. Nil uses http.DefaultClient.
	Client *http.Client
}

func DefaultConfig() Config {
	return Config{
		DownloadFor: 10 * time.Second,
		UploadFor:   10 * time.Second,
		Streams:     4,
		BaseURL:     "https://speed.cloudflare.com",
		MaxBytes:    800_000_000,
		PingSamples: 5,
	}
}

func QuickConfig() Config {
	return Config{
		DownloadFor: 5 * time.Second,
		UploadFor:   5 * time.Second,
		Streams:     2,
		BaseURL:     "https://speed.cloudflare.com",
		MaxBytes:    400_000_000,
		PingSamples: 3,
	}
}

func (c *Config) withDefaults() Config {
	cfg := *c
	if cfg.DownloadFor <= 0 {
		cfg.DownloadFor = 10 * time.Second
	}
	if cfg.UploadFor <= 0 {
		cfg.UploadFor = 10 * time.Second
	}
	if cfg.Streams <= 0 {
		cfg.Streams = 4
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://speed.cloudflare.com"
	}
	if cfg.PingSamples <= 0 {
		cfg.PingSamples = 5
	}
	return cfg
}

type Result struct {
	DownloadMbps float64
	UploadMbps   float64
	LatencyMs    float64
	JitterMs     float64
	Server       string
	At           time.Time
}

type Progress struct {
	Phase       string
	Elapsed     time.Duration
	TotalBytes  int64
	InstantMbps float64
	AvgMbps     float64
}

func FormatMbps(m float64) string {
	if m < 0 {
		m = 0
	}
	switch {
	case m >= 1000:
		return fmt.Sprintf("%.2f Gbps", m/1000)
	case m >= 10:
		return fmt.Sprintf("%.1f Mbps", m)
	default:
		return fmt.Sprintf("%.2f Mbps", m)
	}
}

func Mbps(bytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 || bytes <= 0 {
		return 0
	}
	return float64(bytes) * 8 / elapsed.Seconds() / 1e6
}

func clientFor(cfg Config) *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	return &http.Client{}
}

// Run performs ping -> download -> upload sequentially, invoking report
// (which may be nil) with live progress. It respects ctx cancellation,
// including aborting mid-phase promptly.
func Run(ctx context.Context, cfg Config, report func(Progress)) (Result, error) {
	cfg = cfg.withDefaults()
	client := clientFor(cfg)
	var res Result
	res.Server = cfg.BaseURL
	res.At = time.Now()

	if err := ctx.Err(); err != nil {
		return res, err
	}

	latency, jitter, err := runPing(ctx, client, cfg)
	if err != nil {
		return res, err
	}
	res.LatencyMs = latency
	res.JitterMs = jitter

	down, err := runDownload(ctx, client, cfg, report)
	if err != nil {
		return res, err
	}
	res.DownloadMbps = down

	up, err := runUpload(ctx, client, cfg, report)
	if err != nil {
		return res, err
	}
	res.UploadMbps = up

	return res, nil
}

func runPing(ctx context.Context, client *http.Client, cfg Config) (latencyMs, jitterMs float64, err error) {
	url := cfg.BaseURL + "/__down?bytes=0"
	samples := make([]float64, 0, cfg.PingSamples)
	// The first request opens the connection (DNS, TCP, TLS); measure it only
	// as a warm-up so the samples reflect network latency, not setup.
	for i := 0; i <= cfg.PingSamples; i++ {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, 0, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, 0, fmt.Errorf("ping: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return 0, 0, fmt.Errorf("ping: server returned %s", resp.Status)
		}
		if i > 0 {
			samples = append(samples, float64(time.Since(start).Microseconds())/1000)
		}
		if i < cfg.PingSamples {
			select {
			case <-ctx.Done():
				return 0, 0, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if len(samples) == 0 {
		return 0, 0, fmt.Errorf("ping: no samples collected")
	}
	min := samples[0]
	var sum float64
	for _, s := range samples {
		if s < min {
			min = s
		}
		sum += s
	}
	mean := sum / float64(len(samples))
	var variance float64
	for _, s := range samples {
		d := s - mean
		variance += d * d
	}
	variance /= float64(len(samples))
	return min, math.Sqrt(variance), nil
}

func runDownload(ctx context.Context, client *http.Client, cfg Config, report func(Progress)) (float64, error) {
	phaseCtx, cancel := context.WithTimeout(ctx, cfg.DownloadFor+5*time.Second)
	defer cancel()
	// Hard deadline for the measurement window; workers keep re-requesting
	// until windowCtx expires.
	windowCtx, windowCancel := context.WithTimeout(ctx, cfg.DownloadFor)
	defer windowCancel()

	var total atomic.Int64
	var failures atomic.Int64
	var lastStatus atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < cfg.Streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if windowCtx.Err() != nil || phaseCtx.Err() != nil {
					return
				}
				if cfg.MaxBytes > 0 && total.Load() >= cfg.MaxBytes {
					return
				}
				url := fmt.Sprintf("%s/__down?bytes=%d", cfg.BaseURL, downloadBytesPerRequest)
				req, err := http.NewRequestWithContext(windowCtx, http.MethodGet, url, nil)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					// Window expiry surfaces here as context errors; stop quietly.
					if windowCtx.Err() != nil || phaseCtx.Err() != nil || ctx.Err() != nil {
						return
					}
					failures.Add(1)
					// Transient error: brief backoff, then retry within window.
					select {
					case <-windowCtx.Done():
						return
					case <-time.After(200 * time.Millisecond):
						continue
					}
				}
				if resp.StatusCode >= 400 {
					status := resp.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
					_ = resp.Body.Close()
					if windowCtx.Err() != nil || phaseCtx.Err() != nil || ctx.Err() != nil {
						return
					}
					failures.Add(1)
					lastStatus.Store(int64(status))
					select {
					case <-windowCtx.Done():
						return
					case <-time.After(200 * time.Millisecond):
						continue
					}
				}
				_, _ = io.Copy(io.Discard, &countingReader{r: resp.Body, total: &total, cap: cfg.MaxBytes})
				_ = resp.Body.Close()
			}
		}()
	}

	start := time.Now()
	lastTime := start
	var lastBytes int64
	ticker := time.NewTicker(progressTick)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
loop:
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			n := total.Load()
			inst := Mbps(n-lastBytes, now.Sub(lastTime))
			lastBytes, lastTime = n, now
			if report != nil {
				report(Progress{
					Phase:       PhaseDownload,
					Elapsed:     now.Sub(start),
					TotalBytes:  n,
					InstantMbps: inst,
					AvgMbps:     Mbps(n, now.Sub(start)),
				})
			}
		case <-done:
			break loop
		case <-windowCtx.Done():
			// Measurement window over; wait for in-flight workers to exit.
			// Bound the wait so a hung server can't block forever.
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
			break loop
		case <-ctx.Done():
			<-done
			break loop
		}
	}
	elapsed := time.Since(start)
	if elapsed > cfg.DownloadFor {
		elapsed = cfg.DownloadFor
	}
	if err := ctx.Err(); err != nil && total.Load() == 0 {
		return 0, err
	}
	n := total.Load()
	if n == 0 && failures.Load() > 0 {
		if code := lastStatus.Load(); code != 0 {
			return 0, fmt.Errorf("download: server returned HTTP %d", code)
		}
		return 0, fmt.Errorf("download: no data received (%d failed requests)", failures.Load())
	}
	if report != nil {
		report(Progress{Phase: PhaseDownload, Elapsed: elapsed, TotalBytes: n, InstantMbps: Mbps(n, elapsed), AvgMbps: Mbps(n, elapsed)})
	}
	return Mbps(n, elapsed), nil
}

func runUpload(ctx context.Context, client *http.Client, cfg Config, report func(Progress)) (float64, error) {
	windowCtx, windowCancel := context.WithTimeout(ctx, cfg.UploadFor)
	defer windowCancel()

	var total atomic.Int64
	var failures atomic.Int64
	var lastStatus atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < cfg.Streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if windowCtx.Err() != nil || ctx.Err() != nil {
					return
				}
				if cfg.MaxBytes > 0 && total.Load() >= cfg.MaxBytes {
					return
				}
				body := &countingReader{r: &zeroReader{remaining: uploadBytesPerRequest}, total: &total, cap: cfg.MaxBytes}
				url := cfg.BaseURL + "/__up"
				req, err := http.NewRequestWithContext(windowCtx, http.MethodPost, url, body)
				if err != nil {
					return
				}
				req.ContentLength = uploadBytesPerRequest
				req.Header.Set("Content-Type", "application/octet-stream")
				resp, err := client.Do(req)
				if err != nil {
					if windowCtx.Err() != nil || ctx.Err() != nil {
						return
					}
					failures.Add(1)
					select {
					case <-windowCtx.Done():
						return
					case <-time.After(200 * time.Millisecond):
						continue
					}
				}
				if resp.StatusCode >= 400 {
					status := resp.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
					_ = resp.Body.Close()
					if windowCtx.Err() != nil || ctx.Err() != nil {
						return
					}
					failures.Add(1)
					lastStatus.Store(int64(status))
					select {
					case <-windowCtx.Done():
						return
					case <-time.After(200 * time.Millisecond):
						continue
					}
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64))
				_ = resp.Body.Close()
			}
		}()
	}

	start := time.Now()
	lastTime := start
	var lastBytes int64
	ticker := time.NewTicker(progressTick)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
loop:
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			n := total.Load()
			inst := Mbps(n-lastBytes, now.Sub(lastTime))
			lastBytes, lastTime = n, now
			if report != nil {
				report(Progress{
					Phase:       PhaseUpload,
					Elapsed:     now.Sub(start),
					TotalBytes:  n,
					InstantMbps: inst,
					AvgMbps:     Mbps(n, now.Sub(start)),
				})
			}
		case <-done:
			break loop
		case <-windowCtx.Done():
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
			break loop
		case <-ctx.Done():
			<-done
			break loop
		}
	}
	elapsed := time.Since(start)
	if elapsed > cfg.UploadFor {
		elapsed = cfg.UploadFor
	}
	if err := ctx.Err(); err != nil && total.Load() == 0 {
		return 0, err
	}
	n := total.Load()
	if n == 0 && failures.Load() > 0 {
		if code := lastStatus.Load(); code != 0 {
			return 0, fmt.Errorf("upload: server returned HTTP %d", code)
		}
		return 0, fmt.Errorf("upload: no data received (%d failed requests)", failures.Load())
	}
	if report != nil {
		report(Progress{Phase: PhaseUpload, Elapsed: elapsed, TotalBytes: n, InstantMbps: Mbps(n, elapsed), AvgMbps: Mbps(n, elapsed)})
	}
	return Mbps(n, elapsed), nil
}

type countingReader struct {
	r     io.Reader
	total *atomic.Int64
	cap   int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.cap > 0 {
		if left := c.cap - c.total.Load(); left <= 0 {
			return 0, io.EOF
		} else if int64(len(p)) > left {
			p = p[:left]
		}
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.total.Add(int64(n))
	}
	return n, err
}

// zeroReader yields n zero bytes without allocating a giant buffer.
type zeroReader struct {
	remaining int64
}

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > z.remaining {
		p = p[:z.remaining]
	}
	for i := range p {
		p[i] = 0
	}
	z.remaining -= int64(len(p))
	return len(p), nil
}
