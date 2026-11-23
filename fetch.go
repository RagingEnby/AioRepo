package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	userAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	maxBodyBytes   = 64 << 20
	globalLimit    = 32
	perHostLimit   = 4
	requestTimeout = 30 * time.Second
	maxAttempts    = 3
)

// fetcher makes GET requests with a global and a per-host concurrency limit
// and retries network errors, 429s, and 5xx responses.
type fetcher struct {
	client *http.Client
	global chan struct{}

	mu    sync.Mutex
	hosts map[string]chan struct{}

	// sleep waits between retries. Tests replace it.
	sleep func(context.Context, time.Duration) error
}

func newFetcher() *fetcher {
	return &fetcher{
		client: &http.Client{},
		global: make(chan struct{}, globalLimit),
		hosts:  map[string]chan struct{}{},
		sleep:  sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// validators are the cache headers for a conditional GET.
type validators struct {
	ETag         string
	LastModified string
}

type response struct {
	Body         []byte
	FinalURL     string
	ETag         string
	LastModified string
	NotModified  bool
}

// httpError is an HTTP status that ended a request.
type httpError struct{ Status int }

func (e *httpError) Error() string { return fmt.Sprintf("http %d", e.Status) }

func retryable(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status == http.StatusTooManyRequests || he.Status >= 500
	}
	// Every *url.Error is a net.Error, so check Timeout rather than the type.
	// DNS misses and certificate errors will not fix themselves in seconds.
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, context.DeadlineExceeded)
}

type getOptions struct {
	timeout  time.Duration
	attempts int
	cond     *validators
}

// get fetches rawURL. It retries transient failures with a short backoff.
func (f *fetcher) get(ctx context.Context, rawURL string, opt getOptions) (*response, error) {
	if opt.timeout == 0 {
		opt.timeout = requestTimeout
	}
	if opt.attempts == 0 {
		opt.attempts = maxAttempts
	}
	var err error
	for attempt := 1; attempt <= opt.attempts; attempt++ {
		var resp *response
		resp, err = f.once(ctx, rawURL, opt)
		if err == nil {
			return resp, nil
		}
		if attempt == opt.attempts || !retryable(err) || ctx.Err() != nil {
			break
		}
		if serr := f.sleep(ctx, time.Duration(attempt)*2*time.Second); serr != nil {
			return nil, serr
		}
	}
	return nil, err
}

func (f *fetcher) hostSem(host string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.hosts[host]
	if !ok {
		s = make(chan struct{}, perHostLimit)
		f.hosts[host] = s
	}
	return s
}

func acquire(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fetcher) once(ctx context.Context, rawURL string, opt getOptions) (*response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := acquire(ctx, f.global); err != nil {
		return nil, err
	}
	defer func() { <-f.global }()
	hs := f.hostSem(strings.ToLower(u.Host))
	if err := acquire(ctx, hs); err != nil {
		return nil, err
	}
	defer func() { <-hs }()

	ctx, cancel := context.WithTimeout(ctx, opt.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if opt.cond != nil {
		if opt.cond.ETag != "" {
			req.Header.Set("If-None-Match", opt.cond.ETag)
		}
		if opt.cond.LastModified != "" {
			req.Header.Set("If-Modified-Since", opt.cond.LastModified)
		}
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := &response{
		FinalURL:     resp.Request.URL.String(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	if resp.StatusCode == http.StatusNotModified && opt.cond != nil {
		out.NotModified = true
		return out, nil
	}
	if resp.StatusCode >= 400 {
		return nil, &httpError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("body is larger than %d MB", maxBodyBytes>>20)
	}
	out.Body = body
	return out, nil
}

// getUpgrading tries an http:// URL over https first, then falls back to the
// URL as given.
func (f *fetcher) getUpgrading(ctx context.Context, rawURL string, opt getOptions) (*response, error) {
	if rest, ok := strings.CutPrefix(rawURL, "http://"); ok {
		once := opt
		once.attempts = 1
		if resp, err := f.get(ctx, "https://"+rest, once); err == nil {
			return resp, nil
		}
	}
	return f.get(ctx, rawURL, opt)
}

// normalizeURL returns the key used to spot duplicate source URLs: https,
// lowercase host, no default port, no fragment, and no trailing slash.
func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Scheme = "https"
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		host += ":" + p
	}
	u.Host = host
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String()
}
