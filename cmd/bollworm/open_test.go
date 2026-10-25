package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tlsOpener is newOpener pointed at a test server's certificate.
func tlsOpener(t *testing.T, srv *httptest.Server) *opener {
	t.Helper()
	o := newOpener(true)
	tr := srv.Client().Transport
	o.client.Transport = tr
	return o
}

func TestBoundsAreTheDocumentedValues(t *testing.T) {
	if fetchTimeout != 15*time.Second || maxFetchSize != 16<<20 || maxRedirects != 5 {
		t.Errorf("timeout %v, size %d, redirects %d", fetchTimeout, maxFetchSize, maxRedirects)
	}
	o := newOpener(true)
	if o.client.Timeout != fetchTimeout {
		t.Errorf("client timeout %v", o.client.Timeout)
	}
	if newOpener(false).remote {
		t.Error("remote fetching is off unless asked for")
	}
}

func TestFetch(t *testing.T) {
	var agent string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.UserAgent()
		switch r.URL.Path {
		case "/ok.json":
			_, _ = w.Write([]byte(`{"A": {}}`))
		case "/big.json":
			_, _ = w.Write(make([]byte, maxFetchSize+1))
		case "/exact.json":
			_, _ = w.Write(make([]byte, maxFetchSize))
		case "/down":
			http.Redirect(w, r, "http://"+r.Host+"/ok.json", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	o := tlsOpener(t, srv)

	if b, err := o.open(srv.URL + "/ok.json"); err != nil || string(b) != `{"A": {}}` {
		t.Errorf("ok: %q %v", b, err)
	}
	if agent != "bollworm/dev" {
		t.Errorf("user agent %q", agent)
	}
	if b, err := o.open(srv.URL + "/exact.json"); err != nil || len(b) != maxFetchSize {
		t.Errorf("a document of exactly the limit is read: %d %v", len(b), err)
	}
	cases := map[string]string{
		"/big.json": "larger than 16 MiB",
		"/missing":  "404 Not Found",
		"/down":     "which is not https",
		"/loop":     "more than 5 redirects",
	}
	for path, want := range cases {
		if _, err := o.open(srv.URL + path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", path, err, want)
		}
	}
}

func TestFetchIsOffByDefault(t *testing.T) {
	hit := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	o := newOpener(false)
	o.client.Transport = srv.Client().Transport
	if _, err := o.open(srv.URL + "/x.json"); !errors.Is(err, errRemoteOff) {
		t.Errorf("got %v", err)
	}
	if hit {
		t.Error("the server was contacted")
	}
}

func TestFilesAreAlwaysRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.yaml")
	mustWrite(t, p, []byte("A: {}\n"))
	if b, err := newOpener(false).open(p); err != nil || string(b) != "A: {}\n" {
		t.Errorf("%q %v", b, err)
	}
}

func TestRenderFollowsFileRefs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "schemas", "pet.yaml"), []byte("Pet: {properties: {name: {type: string}}}\n"))
	spec := "openapi: 3.1.0\ncomponents:\n  schemas:\n    Pet: {$ref: 'schemas/pet.yaml#/Pet'}\n"
	mustWrite(t, filepath.Join(dir, "openapi.yaml"), []byte(spec))

	out := filepath.Join(dir, "a.docx")
	if r := cli(t, "", "render", filepath.Join(dir, "openapi.yaml"), "-o", out); r.code != 0 {
		t.Fatalf("file: %+v", r)
	}
	data, _ := os.ReadFile(out)
	isDocx(t, data)

	// From stdin, a relative $ref resolves against the working directory.
	t.Chdir(dir)
	if r := cli(t, spec, "render", "-", "-o", "b.docx"); r.code != 0 {
		t.Fatalf("stdin: %+v", r)
	}
}

func TestRemoteRefsNeedTheFlag(t *testing.T) {
	src := "openapi: 3.1.0\ncomponents:\n  schemas:\n    X: {$ref: 'https://example.com/s.json#/A'}\n"
	r := cli(t, src, "render", "-", "-o", "-")
	want := `bollworm: stdin: line 4: $ref "https://example.com/s.json#/A": fetching it needs the network; pass --allow-remote-refs to allow that, or bundle the document into one file`
	if r.code != exitFailure || !strings.HasPrefix(r.stderr, want) {
		t.Errorf("%+v", r)
	}
	r = cli(t, strings.Replace(src, "https:", "http:", 1), "render", "-", "-o", "-", "--allow-remote-refs")
	if r.code != exitFailure || !strings.Contains(r.stderr, "plain http is not fetched; only https") {
		t.Errorf("http: %+v", r)
	}
}

func TestFetchRetriesPassingFailures(t *testing.T) {
	var calls map[string]int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		n := calls[r.URL.Path]
		switch r.URL.Path {
		case "/flaky":
			if n < fetchAttempts {
				w.Header().Set("Retry-After", "120")
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		case "/limited":
			http.Error(w, "slow down", http.StatusTooManyRequests)
		case "/broken":
			http.Error(w, "nope", http.StatusNotImplemented)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	o := tlsOpener(t, srv)
	var waits []time.Duration
	o.sleep = func(d time.Duration) { waits = append(waits, d) }
	o.jitter = func() float64 { return 1 }

	calls = map[string]int{}
	if b, err := o.open(srv.URL + "/flaky"); err != nil || string(b) != "ok" {
		t.Errorf("flaky: %q %v", b, err)
	}
	if calls["/flaky"] != fetchAttempts || len(waits) != fetchAttempts-1 || waits[0] != maxRetryWait {
		t.Errorf("flaky: %d calls, waits %v", calls["/flaky"], waits)
	}

	_, err := o.open(srv.URL + "/limited")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "tried 3 times") || calls["/limited"] != fetchAttempts {
		t.Errorf("limited: %v after %d calls", err, calls["/limited"])
	}
	for _, path := range []string{"/broken", "/missing"} {
		if _, err := o.open(srv.URL + path); err == nil || strings.Contains(err.Error(), "tried") || calls[path] != 1 {
			t.Errorf("%s is not retried: %v after %d calls", path, err, calls[path])
		}
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"3":                             3 * time.Second,
		"999":                           maxRetryWait,
		"0":                             time.Second,
		"Sun, 25 Oct 2026 12:00:04 GMT": 4 * time.Second,
		"Sun, 25 Oct 2026 13:00:00 GMT": maxRetryWait,
		"Sun, 25 Oct 2026 11:00:00 GMT": time.Second,
		"soon":                          0,
	}
	for h, want := range cases {
		if got := retryAfter(h, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestTimeoutIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	o := tlsOpener(t, srv)
	o.client.Timeout = 50 * time.Millisecond
	o.sleep = func(time.Duration) { t.Error("slept before retrying a timeout") }
	if _, err := o.open(srv.URL + "/slow"); err == nil || calls.Load() != 1 {
		t.Errorf("slow: %v after %d calls", err, calls.Load())
	}
}

func TestBackoffDoublesAndIsJittered(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()
	for _, c := range []struct {
		jitter float64
		want   []time.Duration
	}{
		{1, []time.Duration{time.Second, 2 * time.Second}},
		{0.5, []time.Duration{500 * time.Millisecond, time.Second}},
		{1.49, []time.Duration{1490 * time.Millisecond, 2980 * time.Millisecond}},
	} {
		o := tlsOpener(t, srv)
		var waits []time.Duration
		o.sleep = func(d time.Duration) { waits = append(waits, d) }
		o.jitter = func() float64 { return c.jitter }
		if _, err := o.open(srv.URL + "/x"); err == nil {
			t.Fatal("502 succeeded")
		}
		if !slices.Equal(waits, c.want) {
			t.Errorf("jitter %v: waits %v, want %v", c.jitter, waits, c.want)
		}
	}
	for range 1000 {
		if j := newOpener(true).jitter(); j < 0.5 || j >= 1.5 {
			t.Fatalf("jitter %v outside [0.5, 1.5)", j)
		}
	}
}
