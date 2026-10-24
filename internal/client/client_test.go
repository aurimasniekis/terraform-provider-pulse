package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "pulse_pat_abcd1234_secretsecretsecret" // gitleaks:allow (test literal)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Endpoint: srv.URL, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	return c, &n
}

func TestValidateToken(t *testing.T) {
	for tok, ok := range map[string]bool{
		testToken:                               true,
		"pulse_pat_":                            false,
		"porch_at_x":                            false,
		"pulse_pat_a b":                         false,
		"pulse_pat_" + strings.Repeat("x", 200): false,
	} {
		if err := ValidateToken(tok); (err == nil) != ok {
			t.Errorf("ValidateToken(%q) = %v", tok, err)
		}
	}
}

func TestParseEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:8080":                "http://127.0.0.1:8080/api/admin/v1",
		"http://127.0.0.1:8080/":               "http://127.0.0.1:8080/api/admin/v1",
		"http://127.0.0.1:8080/api/admin/v1":   "http://127.0.0.1:8080/api/admin/v1",
		"https://admin.events.example.com/sub": "https://admin.events.example.com/sub/api/admin/v1",
	} {
		u, err := ParseEndpoint(in)
		if err != nil || u.String() != want {
			t.Errorf("ParseEndpoint(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"127.0.0.1:8080", "ftp://x", "http://u:p@h", "http://h/?q=1"} {
		if _, err := ParseEndpoint(bad); err == nil {
			t.Errorf("ParseEndpoint(%q) accepted", bad)
		}
	}
}

func TestRequestHeadersAndProblem(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.URL.Path != "/api/admin/v1/streams/missing" {
			t.Errorf("request %s %v", r.URL.Path, r.Header)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404,"detail":"stream not found","code":"not_found"}`))
	})
	_, err := c.GetStream(context.Background(), "missing")
	if !IsNotFound(err) || !strings.Contains(err.Error(), "stream not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnauthorizedExplainsLoopback(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"code":"unauthorized","detail":"sign in"}`))
	})
	_, err := c.GetSystemInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "loopback admin listener") {
		t.Fatalf("err = %v", err)
	}
}

func TestRetries(t *testing.T) {
	// Reads are retried on 5xx.
	c, n := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if _, err := c.GetSystemInfo(context.Background()); err == nil || n.Load() != 5 {
		t.Fatalf("GET: err %v after %d attempts, want 5", err, n.Load())
	}
	// Creates are not retried on 5xx (Pulse creates are not idempotent)…
	c, n = newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	if _, err := c.CreateStream(context.Background(), StreamInput{Name: "x"}); err == nil || n.Load() != 1 {
		t.Fatalf("POST: err %v after %d attempts, want 1", err, n.Load())
	}
	// …but are retried after a 429, which Pulse sends before doing anything.
	var calls atomic.Int64
	c, _ = newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"x"}`))
	})
	if s, err := c.CreateStream(context.Background(), StreamInput{Name: "x"}); err != nil || s.Name != "x" || calls.Load() != 2 {
		t.Fatalf("POST after 429: %v %v (%d calls)", s, err, calls.Load())
	}
}

func TestNoRedirects(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/", http.StatusFound)
	})
	_, err := c.GetSystemInfo(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusFound {
		t.Fatalf("a redirect must fail (never followed, never treated as success): %v", err)
	}
}
