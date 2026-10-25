package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
