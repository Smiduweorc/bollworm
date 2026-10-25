package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// files is an Opener over an in-memory set of documents, recording what was
// asked for.
type files struct {
	docs   map[string]string
	opened []string
}

func (f *files) open(loc string) ([]byte, error) {
	f.opened = append(f.opened, loc)
	src, ok := f.docs[loc]
	if !ok {
		return nil, errors.New("no such document")
	}
	return []byte(src), nil
}

// abs makes a test path absolute the way the platform spells it.
func abs(p string) string {
	a, _ := filepath.Abs(filepath.FromSlash(p))
	return a
}

func parseWith(t *testing.T, f *files, loc, src string) *Spec {
	t.Helper()
	s, err := Parse([]byte(src), loc, f.open)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func modelNames(s *Spec) []string {
	var out []string
	for _, m := range s.Models {
		out = append(out, m.Name)
	}
	return out
}

func TestFileRefsResolveAgainstTheirOwnDocument(t *testing.T) {
	f := &files{docs: map[string]string{
		abs("/api/schemas/pet.yaml"):          "Pet:\n  properties:\n    owner: {$ref: 'people/owner.yaml#/Owner'}\n",
		abs("/api/schemas/people/owner.yaml"): "Owner: {properties: {name: {type: string}}}\n",
	}}
	s := parseWith(t, f, abs("/api/openapi.yaml"), `openapi: 3.1.0
paths:
  /pets:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: 'schemas/pet.yaml#/Pet'}
`)
	if !slices.Equal(modelNames(s), []string{"Pet", "Owner"}) {
		t.Errorf("models: %v", modelNames(s))
	}
	pet := ops(s)[0].HTTP.Responses[0].Content[0].Schema
	if pet.Name != "Pet" || pet.Properties[0].Schema.Name != "Owner" {
		t.Errorf("pet: %+v", pet)
	}
	if !slices.Equal(f.opened, []string{abs("/api/schemas/pet.yaml"), abs("/api/schemas/people/owner.yaml")}) {
		t.Errorf("opened %v", f.opened)
	}
}

func TestEachDocumentIsReadOnce(t *testing.T) {
	f := &files{docs: map[string]string{abs("/a/s.yaml"): "A: {type: string}\nB: {type: integer}\n"}}
	parseWith(t, f, abs("/a/o.yaml"), `openapi: 3.1.0
components:
  schemas:
    X: {properties: {a: {$ref: 's.yaml#/A'}, b: {$ref: './s.yaml#/B'}, c: {$ref: 's.yaml#/A'}}}
`)
	if len(f.opened) != 1 {
		t.Errorf("opened %v", f.opened)
	}
}

func TestExternalModelNames(t *testing.T) {
	f := &files{docs: map[string]string{
		abs("/a/one.json"):   `{"Fault": {"type": "object"}, "Whole": 1}`,
		abs("/a/two.json"):   `{"defs": {"Fault": {"type": "string"}}}`,
		abs("/a/whole.json"): `{"type": "integer"}`,
		abs("/a/esc.json"):   `{"a/b": {"type": "boolean"}}`,
	}}
	s := parseWith(t, f, abs("/a/o.yaml"), `openapi: 3.1.0
components:
  schemas:
    Local: {type: string}
    Uses:
      properties:
        a: {$ref: 'one.json#/Fault'}
        b: {$ref: 'two.json#/defs/Fault'}
        c: {$ref: 'whole.json'}
        d: {$ref: 'esc.json#/a~1b'}
        e: {$ref: 'one.json#/Fault'}
        f: {$ref: '#/components/schemas/Local'}
`)
	want := []string{"Local", "Uses", "Fault", "Fault (two.json)", "whole", "a/b"}
	if !slices.Equal(modelNames(s), want) {
		t.Errorf("models: got %q, want %q", modelNames(s), want)
	}
}

func TestComponentThatIsOnlyARef(t *testing.T) {
	f := &files{docs: map[string]string{abs("/a/ext.yaml"): "Thing: {type: object, properties: {x: {}}}\n"}}
	s := parseWith(t, f, abs("/a/o.yaml"), `openapi: 3.1.0
x-defs:
  hidden: {type: string}
components:
  schemas:
    Real: {type: string}
    Alias: {$ref: '#/components/schemas/Real'}
    Named: {$ref: '#/x-defs/hidden'}
    Imported: {$ref: 'ext.yaml#/Thing'}
`)
	// Alias is Real again, so it is not a second model; the other two take
	// the component's key rather than the target's.
	want := []string{"Real", "Named", "Imported"}
	if !slices.Equal(modelNames(s), want) {
		t.Errorf("models: got %q, want %q", modelNames(s), want)
	}
}

func TestRecursionAcrossDocuments(t *testing.T) {
	f := &files{docs: map[string]string{
		abs("/a/x.yaml"): "X: {properties: {y: {$ref: 'y.yaml#/Y'}}}\n",
		abs("/a/y.yaml"): "Y: {properties: {x: {$ref: 'x.yaml#/X'}}}\n",
	}}
	s := parseWith(t, f, abs("/a/o.yaml"), "openapi: 3.1.0\ncomponents:\n  schemas:\n    Root: {$ref: 'x.yaml#/X'}\n")
	root := s.Models[0]
	if root.Name != "Root" || root.Properties[0].Schema.Properties[0].Schema != root {
		t.Error("X and Y should point at each other across documents")
	}
}

func TestRefusalsInsideReferencedDocumentsNameTheDocument(t *testing.T) {
	f := &files{docs: map[string]string{
		abs("/a/bad.yaml"):    "A:\n  type: string\n  b: {$ref: '#/Missing'}\n",
		abs("/a/broken.yaml"): "A: [1\n",
		abs("/a/list.yaml"):   "A: [1, 2]\n",
	}}
	cases := map[string]string{
		"bad.yaml#/A/b":   abs("/a/bad.yaml") + `: line 3: $ref "#/Missing" does not resolve: nothing at "Missing"`,
		"broken.yaml#/A":  abs("/a/broken.yaml") + ": line 1: did not find expected ',' or ']'",
		"list.yaml#/A":    abs("/a/list.yaml") + ": line 1: a schema must be a mapping",
		"missing.yaml#/A": `line 4: $ref "missing.yaml#/A": no such document`,
	}
	for ref, want := range cases {
		_, err := Parse([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    X: {$ref: '"+ref+"'}\n"), abs("/a/o.yaml"), f.open)
		if err == nil || err.Error() != want {
			t.Errorf("%s:\n got %v\nwant %s", ref, err, want)
		}
	}
}

func TestWithoutAnOpenerOtherDocumentsAreRefused(t *testing.T) {
	_, err := Parse([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    X: {$ref: 'other.yaml#/A'}\n"), abs("/a/o.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), `line 4: $ref "other.yaml#/A" points outside this file`) {
		t.Errorf("got %v", err)
	}
}

func TestDocumentCountIsBounded(t *testing.T) {
	f := &files{docs: map[string]string{}}
	var props strings.Builder
	for i := range maxDocuments + 1 {
		name := fmt.Sprintf("d%d.yaml", i)
		f.docs[abs("/a/"+name)] = "A: {type: string}\n"
		fmt.Fprintf(&props, "        p%d: {$ref: '%s#/A'}\n", i, name)
	}
	_, err := Parse([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    X:\n      properties:\n"+props.String()), abs("/a/o.yaml"), f.open)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("would be document number %d; more than %d is refused", maxDocuments+1, maxDocuments)) {
		t.Errorf("got %v", err)
	}
	if len(f.opened) != maxDocuments-1 {
		t.Errorf("opened %d documents before refusing, want %d", len(f.opened), maxDocuments-1)
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		base, target, want, err string
	}{
		{abs("/api/o.yaml"), "s.yaml", abs("/api/s.yaml"), ""},
		{abs("/api/o.yaml"), "../shared/s.yaml", abs("/shared/s.yaml"), ""},
		{abs("/api/o.yaml"), "dir%20with%20space/s.yaml", abs("/api/dir with space/s.yaml"), ""},
		{abs("/api/o.yaml"), "https://example.com/s.json", "https://example.com/s.json", ""},
		{abs("/api/o.yaml"), "HTTPS://example.com/s.json", "https://example.com/s.json", ""},
		{abs("/api/o.yaml"), "http://example.com/s.json", "", "plain http is not fetched; only https"},
		{abs("/api/o.yaml"), "ftp://example.com/s.json", "", "only file paths and https URLs are followed"},
		{"https://example.com/api/o.json", "s.json", "https://example.com/api/s.json", ""},
		{"https://example.com/api/o.json", "../s.json", "https://example.com/s.json", ""},
		{"https://example.com/api/o.json", "/etc/passwd", "https://example.com/etc/passwd", ""},
		{"https://example.com/api/o.json", "https://other.example.com/s.json", "https://other.example.com/s.json", ""},
		{"https://example.com/api/o.json", "file:///etc/passwd", "", "a document fetched over the network cannot refer to file:///etc/passwd"},
		{"https://example.com/api/o.json", "http://example.com/s.json", "", "a document fetched over the network cannot refer to http://example.com/s.json"},
	}
	for _, c := range cases {
		got, err := locate(c.base, c.target)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("locate(%q, %q): got %q, %v; want error %q", c.base, c.target, got, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("locate(%q, %q) = %q, %v; want %q", c.base, c.target, got, err, c.want)
		}
	}
	if got, err := locate(abs("/api/o.yaml"), "file:///srv/s.yaml"); err != nil || got != filepath.FromSlash("/srv/s.yaml") {
		t.Errorf("file URL: %q %v", got, err)
	}
}

func TestFetchedDocumentsCannotReadFiles(t *testing.T) {
	f := &files{docs: map[string]string{
		"https://example.com/api/s.json": `{"A": {"$ref": "file:///etc/passwd"}}`,
	}}
	_, err := Parse([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    X: {$ref: 'https://example.com/api/s.json#/A'}\n"), abs("/a/o.yaml"), f.open)
	if err == nil || !strings.Contains(err.Error(), "https://example.com/api/s.json: line 1: $ref \"file:///etc/passwd\": a document fetched over the network cannot refer to file:///etc/passwd") {
		t.Errorf("got %v", err)
	}
	if slices.Contains(f.opened, "/etc/passwd") {
		t.Error("the file was opened")
	}
}

func TestStravaShapedSpec(t *testing.T) {
	// The shape of testdata/strava.json: every model in a document of its
	// own, referenced by absolute https URL, referring to each other the
	// same way.
	f := &files{docs: map[string]string{
		"https://example.com/swagger/athlete.json": `{"DetailedAthlete": {"properties": {"clubs": {"type": "array", "items": {"$ref": "https://example.com/swagger/club.json#/SummaryClub"}}}}}`,
		"https://example.com/swagger/club.json":    `{"SummaryClub": {"properties": {"name": {"type": "string"}}}}`,
		"https://example.com/swagger/fault.json":   `{"Fault": {"properties": {"message": {"type": "string"}}}}`,
	}}
	s := parseWith(t, f, abs("/a/strava.json"), `{
  "openapi": "3.0.0",
  "paths": {"/athlete": {"get": {"responses": {
    "200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "https://example.com/swagger/athlete.json#/DetailedAthlete"}}}},
    "default": {"description": "err", "content": {"application/json": {"schema": {"$ref": "https://example.com/swagger/fault.json#/Fault"}}}}
  }}}}
}`)
	if want := []string{"DetailedAthlete", "SummaryClub", "Fault"}; !slices.Equal(modelNames(s), want) {
		t.Errorf("models: got %q, want %q", modelNames(s), want)
	}
}

func TestStravaNeedsItsRemoteDocuments(t *testing.T) {
	src, err := os.ReadFile("../../testdata/strava.json")
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("offline")
	_, err = Parse(src, abs("testdata/strava.json"), func(string) ([]byte, error) { return nil, refused })
	want := `line 42: $ref "https://developers.strava.com/swagger/activity_stats.json#/ActivityStats": offline`
	var errs Errors
	if !errors.As(err, &errs) || errs[0].Error() != want {
		t.Fatalf("got %v", err)
	}
	// Every unreachable document is reported, each once.
	seen := map[string]bool{}
	for _, e := range errs {
		doc, _, _ := strings.Cut(strings.Split(e.Msg, `"`)[1], "#")
		if seen[doc] {
			t.Errorf("%s reported twice", doc)
		}
		seen[doc] = true
	}
	if len(seen) < 10 {
		t.Errorf("only %d documents reported", len(seen))
	}
}
