package render

import (
	"archive/zip"
	"bytes"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/spec"
)

// extras is testdata/extras.yaml, which make validate also checks against
// the schema.
func extras(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../testdata/extras.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestExtrasAreDescribed(t *testing.T) {
	src := extras(t)
	doc := document(t, src)
	text := visible(doc)
	mustContain(t, text,
		"Introduction\nSee also: The guide (https://docs.example.com).\n",
		"Pets\nSee also: https://docs.example.com/pets.\n",
		"See also: Creating pets (https://docs.example.com/create).\n",
		"Serialized with style pipeDelimited, not exploded: values are sent comma-separated in one parameter.",
		"Sent as application/json.",
		"How fields are sent\nField | Content type | Notes | \nphoto | image/png | Header X-Rate (integer): Upload rate. | \nmeta | Default | Serialized with style deepObject, exploded: each value or property is sent as its own parameter. | \n",
		"Links from the 201 response\nLink | Operation | Values passed | Description | \nGetPet | getPet | id = $response.body#/id | Fetch what was made. | \n",
		"Callback: onAdopted\nThe API sends this request to a URL taken from the call above, which your server receives.\nAdopted: POST  {$request.body#/callbackUrl}\nRequest body\n",
		"Responses\nStatus | Description | Body | \n204 No Content | Received | None | \n",
		"The kind property says which one a value is:\nValue of kind | Model | \ncat | Cat | \ndog | Dog | \nbird | #/components/schemas/Bird | \n",
		"In XML, an element named pet, in the namespace https://example.com/ns.\n",
		"See also: https://docs.example.com/pet.\n",
		"In XML, an attribute.",
	)
	// The callback's operation is no chapter of its own.
	if n := strings.Count(text, "Adopted:"); n != 1 {
		t.Errorf("callback operation printed %d times", n)
	}
	// The link names its target's section.
	mustContain(t, doc, `w:anchor="Op_getPet"`, `w:name="Op_getPet"`)
	linksResolve(t, "extras", doc)
	// The server's URL is not taken as the base for relative links.
	if strings.Contains(rels(t, src, Options{Paper: docx.A4}), "rules.html") || !strings.Contains(text, "See the rules (/rules.html) and above (#top).") {
		t.Errorf("links: %q", text)
	}
}

func TestLinkBase(t *testing.T) {
	src := head + "info: {title: T, version: '1', description: 'Read [this](docs/a.html).'}\n"
	src = strings.Replace(src, "info: {title: T, version: '1'}\n", "", 1)
	base, _ := url.Parse("https://example.com/guide/")
	mustContain(t, rels(t, src, Options{Paper: docx.A4, LinkBase: base}), `Target="https://example.com/guide/docs/a.html"`)
	if w := warnings(t, src, Options{Paper: docx.A4, LinkBase: base}); len(w) != 0 {
		t.Errorf("with a base: %q", w)
	}

	// Without a base the link is printed after its text, even when a
	// server URL is there to guess from, and each such link is warned
	// about once.
	src += "servers: [{url: 'https://api.example.com/'}]\npaths:\n  /a: {get: {description: 'Also [this](docs/a.html) and [that](#x).', responses: {}}}\n"
	mustContain(t, visible(document(t, src)), "Read this (docs/a.html).")
	want := []string{`relative link "docs/a.html" is printed as text; pass --link-base to make it a link`}
	if w := warnings(t, src, Options{Paper: docx.A4}); !slices.Equal(w, want) {
		t.Errorf("warnings %q", w)
	}
}

func warnings(t *testing.T, src string, opts Options) []string {
	t.Helper()
	s, err := spec.Parse([]byte(src), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, w, err := Render(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// rels renders src and returns word/_rels/document.xml.rels, where the
// targets of hyperlinks are.
func rels(t *testing.T, src string, opts Options) string {
	t.Helper()
	s, err := spec.Parse([]byte(src), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _, err := Render(s, opts)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "word/_rels/document.xml.rels" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatal("no document relationships")
	return ""
}

func TestOperationsUnderTheirOtherTags(t *testing.T) {
	doc := document(t, head+`paths:
  /a: {get: {summary: Get A, tags: [One, Two], responses: {}}}
  /b: {get: {summary: Get B, tags: [Two], responses: {}}}
  /c: {get: {summary: Get C, tags: [One, Three], responses: {}}}
`)
	text := visible(doc)
	mustContain(t, text,
		"Two\nThese endpoints belong here too, and are described in the chapter of their first tag:\nGet A\nGet B\n",
		"Three\nThese endpoints belong here, and are described in the chapter of their first tag:\nGet C\n",
	)
	if n := strings.Count(text, "GET  /a"); n != 1 {
		t.Errorf("Get A described %d times", n)
	}
	linksResolve(t, "tags", doc)
}

func TestTitlesKeepEllipsesAndAbbreviations(t *testing.T) {
	for summary, want := range map[string]string{
		"List pets.":        "List pets",
		"Wait...":           "Wait...",
		"Cats, dogs, etc.":  "Cats, dogs, etc.",
		"Made by Acme Inc.": "Made by Acme Inc.",
		"Upgrade to v2.":    "Upgrade to v2.",
	} {
		if got := operationTitle(&spec.Operation{Summary: summary}); got != want {
			t.Errorf("%q: got %q, want %q", summary, got, want)
		}
	}
}

func TestTOCDepth(t *testing.T) {
	src := head + "paths:\n  /a: {get: {summary: A, parameters: [{name: q, in: query}], responses: {}}}\n"
	for depth, want := range map[int]string{0: `TOC \o "1-2"`, 1: `TOC \o "1-1"`, 3: `TOC \o "1-3"`} {
		s, err := spec.Parse([]byte(src), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := Render(s, Options{Paper: docx.A4, TOCDepth: depth})
		if err != nil {
			t.Fatal(err)
		}
		doc := wellFormed(t, data)
		mustContain(t, doc, want)
		if has := strings.Contains(doc, `w:val="TOC3"`); has != (depth == 3) {
			t.Errorf("depth %d: level-3 entries %v", depth, has)
		}
		linksResolve(t, "toc", doc)
	}
}

func TestSchemaFactsAreSpelledOut(t *testing.T) {
	text := visible(document(t, head+`components:
  schemas:
    Strict:
      type: object
      additionalProperties: false
      properties:
        gone: false
        n: {type: integer, examples: [1, 2]}
        inner: {type: object, additionalProperties: false, properties: {x: {type: string}}}
      examples: [{n: 1}, {n: 2}]
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          links:
            L: {operationId: a2, parameters: {obj: {z: 1, a: 2}, long: {zebra: "a value long enough", aardvark: "to need more than sixty characters"}}, requestBody: {z: 1, a: 2}}
  /b: {get: {operationId: a2, summary: Fetch B, responses: {}}}
`))
	mustContain(t, text,
		"gone | nothing (no value is allowed) |",
		"Examples: 1, 2.",
		"No properties other than the listed ones are allowed.",
		"No properties other than these are allowed.\nExample 1\n",
		"Example 2\n",
		"L | Fetch B | obj = ",
		`obj = {"z":1,"a":2}`,
		// Too long for one cell line, but still one line, in the author's order.
		`long = { "zebra": "a value long enough", "aardvark": "to need more than sixty characters" }`,
		`Request body = {"z":1,"a":2}`,
	)
}
