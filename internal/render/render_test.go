package render

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/spec"
	"gopkg.in/yaml.v3"
)

// document renders src and returns word/document.xml, after checking every
// part of the package is well-formed.
func document(t testing.TB, src string) string {
	t.Helper()
	s, err := spec.Parse([]byte(src), "", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := Render(s, docx.A4)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return wellFormed(t, data)
}

func wellFormed(t testing.TB, data []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var doc string
	for _, f := range z.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		dec := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
		if f.Name == "word/document.xml" {
			doc = string(b)
		}
	}
	return doc
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

// visible is the visible text of a document.xml fragment: paragraphs and
// table rows on their own lines, a row's cells each followed by " | ".
func visible(xmlDoc string) string {
	xmlDoc = regexp.MustCompile(`<w:instrText[^>]*>[^<]*</w:instrText>`).ReplaceAllString(xmlDoc, "")
	s := strings.NewReplacer("</w:p></w:tc>", " | ", "</w:p>", "\n", "</w:tr>", "\n", "<w:br/>", "/").Replace(xmlDoc)
	s = tagRe.ReplaceAllString(s, "")
	r := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&amp;", "&")
	return strings.TrimLeft(r.Replace(s), "\n")
}

func mustContain(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Errorf("missing %q", n)
		}
	}
}

const head = "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n"

func TestTitlePage(t *testing.T) {
	got := visible(document(t, `openapi: 3.1.0
info:
  title: Lending Library
  version: 2.3.0
  summary: Loans and holds.
  termsOfService: https://example.com/tos
  contact: {name: Desk, email: desk@example.com}
  license: {name: MIT, identifier: MIT}
`))
	mustContain(t, got, "Lending Library\n", "API reference, version 2.3.0\n", "Loans and holds.\n",
		"Contact: Desk, desk@example.com\n", "Licence: MIT\n", "Terms of service: https://example.com/tos\n", "Contents\n")
}

func TestMissingTitleFallsBack(t *testing.T) {
	got := visible(document(t, "openapi: 3.1.0\ninfo: {title: '  '}\n"))
	if !strings.HasPrefix(got, "API reference\nAPI reference\n") {
		t.Errorf("got %q", got)
	}
}

func TestContentsMatchTheBodyInOrder(t *testing.T) {
	src, err := os.ReadFile("../../testdata/features.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc := document(t, string(src))
	toc := regexp.MustCompile(`<w:pStyle w:val="TOC(\d)"/>.*?<w:hyperlink w:anchor="([^"]+)"`).FindAllStringSubmatch(doc, -1)
	heads := regexp.MustCompile(`<w:pStyle w:val="Heading([12])"/></w:pPr><w:bookmarkStart w:id="\d+" w:name="([^"]+)"/>`).FindAllStringSubmatch(doc, -1)
	if len(toc) == 0 || len(toc) != len(heads) {
		t.Fatalf("%d contents entries for %d headings", len(toc), len(heads))
	}
	for i := range toc {
		if toc[i][1] != heads[i][1] || toc[i][2] != heads[i][2] {
			t.Errorf("entry %d: contents %v, body %v", i, toc[i][1:], heads[i][1:])
		}
	}
}

func TestEveryInternalLinkHasATarget(t *testing.T) {
	for _, name := range []string{"aur.json", "petstore3.json", "features.yaml"} {
		src, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		doc := document(t, string(src))
		marks := map[string]bool{}
		for _, m := range regexp.MustCompile(`w:bookmarkStart w:id="\d+" w:name="([^"]+)"`).FindAllStringSubmatch(doc, -1) {
			if marks[m[1]] {
				t.Errorf("%s: bookmark %s defined twice", name, m[1])
			}
			if len(m[1]) > 40 || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(m[1]) {
				t.Errorf("%s: bookmark name %q is not one Word accepts", name, m[1])
			}
			marks[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`w:anchor="([^"]+)"`).FindAllStringSubmatch(doc, -1) {
			if !marks[m[1]] {
				t.Errorf("%s: link to missing bookmark %s", name, m[1])
			}
		}
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	src, _ := os.ReadFile("../../testdata/features.yaml")
	s, _ := spec.Parse(src, "", nil)
	a, _ := Render(s, docx.A4)
	s, _ = spec.Parse(src, "", nil)
	b, _ := Render(s, docx.A4)
	if !bytes.Equal(a, b) {
		t.Error("the same spec rendered twice gave different bytes")
	}
}

func TestOperationSection(t *testing.T) {
	got := visible(document(t, head+`
paths:
  /pets/{id}:
    get:
      tags: [Pets]
      summary: Get a pet.
      description: Returns **one** pet.
      parameters:
        - {name: id, in: path, required: true, description: The id, schema: {type: integer, format: int64, minimum: 1}}
        - {name: fields, in: query, schema: {type: array, items: {type: string}}, example: [a, b]}
      responses:
        "200":
          description: The pet
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Pet'}
        "404": {description: Not here}
        "2XX": {description: Fine}
        "599": {description: Odd}
        default: {description: Anything else}
components:
  schemas:
    Pet: {type: object, properties: {name: {type: string}}}
`))
	mustContain(t, got,
		"Pets\n", "Get a pet\n", "GET  /pets/{id}\n", "Returns one pet.\n", "Parameters\n",
		"id | path | integer (int64) | Yes | The id\nAt least 1. | ",
		`fields | query | array of string | No | Example: ["a","b"]. | `,
		"Responses\n", "200 OK | The pet | Pet (application/json) | ",
		"404 Not Found | Not here | None | ", "2XX Success | Fine | None | ",
		"599 | Odd | None | ", "Any other | Anything else | None | ")
	if strings.Contains(got, "Authentication:") {
		t.Error("no authentication line when the document has no schemes")
	}
}

func TestDuplicateTitlesAreToldApart(t *testing.T) {
	got := visible(document(t, head+`
paths:
  /a:
    get: {summary: Lookup}
    post: {summary: Lookup}
  /b:
    get: {summary: Other}
`))
	mustContain(t, got, "Lookup (GET)\n", "Lookup (POST)\n", "Other\n")
	if strings.Contains(got, "Other (GET)") {
		t.Error("a unique title keeps its plain form")
	}
}

func TestOperationTitleFallbacks(t *testing.T) {
	got := visible(document(t, head+"paths:\n  /a:\n    get: {operationId: listA}\n    delete: {}\n"))
	mustContain(t, got, "listA\n", "DELETE /a\n")
}

func TestUntaggedGroupName(t *testing.T) {
	only := visible(document(t, head+"paths:\n  /a:\n    get: {summary: A}\n"))
	mixed := visible(document(t, head+"paths:\n  /a:\n    get: {summary: A}\n  /b:\n    get: {summary: B, tags: [Tagged]}\n"))
	if !strings.Contains(only, "\nEndpoints\n") || strings.Contains(only, "Other endpoints") {
		t.Error("the only group is plain Endpoints")
	}
	mustContain(t, mixed, "\nTagged\n", "\nOther endpoints\n")
}

func TestDeprecatedOperation(t *testing.T) {
	got := visible(document(t, head+"paths:\n  /a:\n    get: {summary: A, deprecated: true}\n"))
	mustContain(t, got, "Deprecated. This endpoint is still served")
}

func TestAuthentication(t *testing.T) {
	got := visible(document(t, head+`
security: [{key: []}]
paths:
  /inherit:
    get: {summary: Inherit}
  /open:
    get: {summary: Open, security: []}
  /both:
    get:
      summary: Both
      security: [{oauth: [read, write], key: []}, {}]
webhooks:
  silent:
    post: {summary: Silent}
components:
  securitySchemes:
    key: {type: apiKey, in: cookie, name: sid, description: From the login page.}
    weird: {type: apiKey, in: somewhere, name: x}
    bearer: {type: http, scheme: Bearer}
    bearerJWT: {type: http, scheme: bearer, bearerFormat: JWT}
    basic: {type: http, scheme: basic}
    digest: {type: http, scheme: digest}
    oauth:
      type: oauth2
      flows:
        password: {tokenUrl: https://example.com/t, refreshUrl: https://example.com/r, scopes: {read: Read, write: Write}}
    oauthTwo:
      type: oauth2
      flows:
        implicit: {authorizationUrl: https://example.com/a, scopes: {read: Read}}
        deviceAuthorization: {tokenUrl: https://example.com/t, scopes: {read: Read again}}
        custom: {tokenUrl: https://example.com/t, scopes: {}}
    oidc: {type: openIdConnect, openIdConnectUrl: https://example.com/oidc}
    mtls: {type: mutualTLS}
    future: {type: quantum}
`))
	mustContain(t, got,
		"Unless an endpoint says otherwise, requests are authenticated with key.\n",
		"An API key sent in the sid cookie.\nFrom the login page.\n",
		"An API key sent in the x somewhere.\n",
		"A bearer token sent in the Authorization header.\n",
		"A bearer token sent in the Authorization header (JWT).\n",
		"HTTP Basic authentication",
		"HTTP authentication with the digest scheme.\n",
		"OAuth 2.0, using the flow below.\n", "Flow | Authorization URL | Token URL | Refresh URL | ", "Password |  | https://example.com/t | https://example.com/r | ",
		"OAuth 2.0, using any of the flows below.\n", "Implicit | https://example.com/a |  | ",
		"Device authorization |  | https://example.com/t | ", "custom |  | https://example.com/t | ",
		"OpenID Connect, configured from https://example.com/oidc.\n",
		"Mutual TLS: the client presents a certificate when it connects.\n",
		"Authentication of type quantum.\n",
		"Inherit\nGET  /inherit\nAuthentication: key.\n",
		"Open\nGET  /open\nAuthentication: none.\n",
		"Authentication: oauth (scopes read, write) together with key, or no authentication.\n",
	)
	// The oauthTwo scopes table lists "read" once although two flows carry it.
	if strings.Count(got, "read | Read") != 2 || strings.Contains(got, "Read again") {
		t.Error("scopes are listed once per scheme, first description wins")
	}
	silent := got[strings.LastIndex(got, "Silent\n"):]
	if strings.Contains(silent, "Authentication:") {
		t.Error("a webhook without its own security has no authentication line")
	}
	mustContain(t, silent, "POST  to your webhook URL\nWebhook: silent\n")
}

func TestSchemeLinksJumpToTheirSection(t *testing.T) {
	doc := document(t, head+"security: [{my-key: []}]\ncomponents:\n  securitySchemes:\n    my-key: {type: apiKey, in: header, name: K}\n")
	mustContain(t, doc, `w:name="Auth_my_key"`, `w:anchor="Auth_my_key"`)
}

func TestRequestBodies(t *testing.T) {
	got := visible(document(t, head+`
paths:
  /upload:
    post:
      summary: Upload
      requestBody:
        description: The file and its label.
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [file]
              properties:
                file: {type: string, format: binary}
                label: {type: string}
          application/xml:
            schema: {$ref: '#/components/schemas/Pet'}
            example: "<pet><name>Rex</name></pet>"
          application/octet-stream:
            schema: {type: string, format: binary}
          image/png: {}
  /pets:
    post:
      summary: Create
      requestBody:
        content:
          application/json: {schema: {$ref: '#/components/schemas/Pet'}}
          application/x-www-form-urlencoded: {schema: {$ref: '#/components/schemas/Pet'}}
components:
  schemas:
    Pet: {type: object, properties: {name: {type: string}}}
`))
	mustContain(t, got,
		"A request body is required.\nThe file and its label.\n",
		"Content type: multipart/form-data. The body is an object with these fields:\n",
		"file | string (binary) | Yes |  | ", "label | string | No |  | ",
		"Content type: application/xml. Body: Pet.\n",
		"Example request body\n<pet><name>Rex</name></pet>\n",
		"Content type: application/octet-stream. Body: string (binary).\n",
		"Content type: image/png.\n",
		"The request body is optional.\nContent type: application/json, application/x-www-form-urlencoded. Body: Pet.\n",
	)
}

func TestResponseDetails(t *testing.T) {
	got := visible(document(t, head+`
paths:
  /a:
    get:
      summary: A
      responses:
        "200":
          description: OK
          headers:
            X-Rate: {description: Calls left., schema: {type: integer}}
            X-Plain: {required: true}
          content:
            application/json:
              schema:
                type: object
                properties: {n: {type: integer}}
              examples:
                one: {summary: One item., value: {n: 1}}
                far: {externalValue: https://example.com/e.json}
            text/csv:
              schema: {type: string}
              example: "n\n1\n"
`))
	mustContain(t, got,
		"200 OK | OK\nHeader X-Rate (integer): Calls left. Header X-Plain (always sent). | object (application/json)/string (text/csv) | ",
		"Fields of the 200 response\n", "n | integer | No |  | ",
		"Example 200 response (application/json): One item\n{/  \"n\": 1/}\n",
		"Example 200 response (application/json): far\nPublished at https://example.com/e.json\n",
		"Example 200 response (text/csv)\nn/1\n",
	)
}

func TestDataModels(t *testing.T) {
	got := visible(document(t, head+`
components:
  schemas:
    Pet:
      description: A pet.
      deprecated: true
      required: [name]
      properties:
        name: {type: string, minLength: 1, maxLength: 1}
        owner:
          type: object
          properties:
            address:
              type: object
              properties:
                city: {type: string}
        tags: {type: array, items: {type: object, properties: {label: {type: string}}}}
        kind: {$ref: '#/components/schemas/Kind'}
        friend: {$ref: '#/components/schemas/Pet'}
      example: {name: Rex}
    Kind: {type: string, enum: [cat, dog], example: cat}
    Shape:
      oneOf: [{$ref: '#/components/schemas/Pet'}, {type: string}]
    Either:
      anyOf: [{type: integer}]
    Empty: {}
`))
	mustContain(t, got,
		"Data models\n", "Pet\nDeprecated.\nA pet.\n",
		"name | string | Yes | Exactly 1 character. | ",
		"owner | object | No |  | ", "owner.address | object | No |  | ", "owner.address.city | string | No |  | ",
		"tags | array of object | No |  | ", "tags[].label | string | No |  | ",
		"kind | Kind | No |  | ", "friend | Pet | No |  | ",
		"Example\n{/  \"name\": \"Rex\"/}\n",
		"Kind\nType: string. Allowed values: \"cat\", \"dog\". Example: \"cat\".\n",
		"Shape is one of the following:\nPet\nstring\n",
		"Either matches at least one of the following:\ninteger\n",
		"Empty\nType: any.\n",
	)
	if strings.Contains(got, "kind | Kind | No | Allowed values") {
		t.Error("a field that is a model does not repeat the model's constraints")
	}
}

func TestDeepAnonymousNestingStops(t *testing.T) {
	got := visible(document(t, head+`
components:
  schemas:
    Deep:
      properties:
        a: {properties: {b: {properties: {c: {properties: {d: {properties: {e: {type: string}}}}}}}}}
`))
	mustContain(t, got, "a.b.c.d | object |")
	if strings.Contains(got, "a.b.c.d.e") {
		t.Errorf("nesting past %d levels is not spelled out", maxFieldDepth)
	}
}

func TestAllOfMergesFields(t *testing.T) {
	got := visible(document(t, head+`
components:
  schemas:
    Base: {properties: {id: {type: integer}, name: {type: string}}}
    Item:
      allOf:
        - $ref: '#/components/schemas/Base'
        - required: [name]
          properties: {name: {type: string}, extra: {type: boolean}}
`))
	item := got[strings.LastIndex(got, "\nItem\n"):]
	mustContain(t, item, "id | integer | No", "name | string | Yes", "extra | boolean | No")
	if strings.Count(item, "name | string") != 1 {
		t.Error("a field declared twice appears once")
	}
}

func TestIntroductionAndServers(t *testing.T) {
	one := visible(document(t, head+"servers: [{url: https://example.com, description: Prod}]\n"))
	mustContain(t, one, "Introduction\n", "Servers\n", "Requests are made to the following base URL.",
		"https://example.com | Prod | ")
	two := visible(document(t, head+"servers: [{url: /a}, {url: /b, variables: {v: {default: x}}}]\n"))
	mustContain(t, two, "Requests can be made to any of the following base URLs.", "Variables in /b\n", "v | x | Any |  | ")
	none := visible(document(t, head))
	if strings.Contains(none, "Introduction") {
		t.Error("no introduction without a description or servers")
	}
}

func TestDocumentWithNothingStillRenders(t *testing.T) {
	got := visible(document(t, "openapi: 3.0.0\n"))
	if got != "API reference\nAPI reference\nContents\n\n" {
		t.Errorf("got %q", got)
	}
}

func TestHostileTextStaysText(t *testing.T) {
	doc := document(t, head+`
paths:
  /a:
    get:
      summary: "</w:t><w:p> & \u0001 bell"
      description: "<script>alert(1)</script>\n\nsee [x](javascript:alert(1))"
`)
	got := visible(doc)
	mustContain(t, got, "</w:t><w:p> &  bell\n", "<script>alert(1)</script>\n", "see x (javascript:alert(1))\n")
	if strings.Contains(doc, "javascript:alert(1)\" TargetMode") {
		t.Error("a javascript: URL must never become a link")
	}
}

func TestTypeOf(t *testing.T) {
	model := &spec.Schema{Name: "Pet"}
	r := &renderer{models: map[*spec.Schema]string{model: "Model_Pet"}}
	str := &spec.Schema{Types: []string{"string"}}
	cases := []struct {
		s    *spec.Schema
		want string
	}{
		{nil, "any"},
		{&spec.Schema{}, "any"},
		{&spec.Schema{Types: []string{"null"}}, "null"},
		{str, "string"},
		{&spec.Schema{Types: []string{"string"}, Format: "uuid"}, "string (uuid)"},
		{&spec.Schema{Types: []string{"string", "integer"}}, "string or integer"},
		{&spec.Schema{Types: []string{"string", "null"}}, "string or null"},
		{&spec.Schema{Types: []string{"array"}}, "array"},
		{&spec.Schema{Types: []string{"array"}, Items: model}, "array of Pet"},
		{&spec.Schema{Items: str}, "array of string"},
		{&spec.Schema{Types: []string{"object"}}, "object"},
		{&spec.Schema{Properties: []*spec.Property{{Name: "a"}}}, "object"},
		{&spec.Schema{Types: []string{"object"}, AdditionalProperties: str}, "map of string"},
		{&spec.Schema{Types: []string{"object"}, AdditionalProperties: str, Properties: []*spec.Property{{Name: "a"}}}, "object"},
		{model, "Pet"},
		{&spec.Schema{AllOf: []*spec.Schema{model}}, "Pet"},
		{&spec.Schema{AllOf: []*spec.Schema{model}, Types: []string{"null"}}, "Pet or null"},
		{&spec.Schema{AllOf: []*spec.Schema{model, str}}, "all of Pet and string"},
		{&spec.Schema{OneOf: []*spec.Schema{model, str}}, "one of Pet or string"},
		{&spec.Schema{AnyOf: []*spec.Schema{str}}, "any of string"},
		{&spec.Schema{Const: &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"}}, `constant "x"`},
	}
	for _, c := range cases {
		if got := plain(r.typeOf(c.s)); got != c.want {
			t.Errorf("typeOf(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
	if in := r.typeOf(model); in[0].Anchor != "Model_Pet" {
		t.Error("a model links to its section")
	}
	if got := plain(r.typeOfInner(model)); got != "any" {
		t.Errorf("a model's own section describes it without linking itself, got %q", got)
	}
}

func TestDetails(t *testing.T) {
	n := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v} }
	cases := []struct {
		s    *spec.Schema
		want string
	}{
		{nil, ""},
		{&spec.Schema{}, ""},
		{&spec.Schema{Minimum: "1"}, "At least 1."},
		{&spec.Schema{Maximum: "9"}, "At most 9."},
		{&spec.Schema{Minimum: "1", Maximum: "9"}, "At least 1 and at most 9."},
		{&spec.Schema{ExclusiveMinimum: "0", ExclusiveMaximum: "9"}, "Greater than 0 and less than 9."},
		{&spec.Schema{MinLength: "0"}, ""},
		{&spec.Schema{MinLength: "1"}, "At least 1 character."},
		{&spec.Schema{MinLength: "2"}, "At least 2 characters."},
		{&spec.Schema{MaxLength: "1"}, "At most 1 character."},
		{&spec.Schema{MinLength: "3", MaxLength: "3"}, "Exactly 3 characters."},
		{&spec.Schema{MinLength: "0", MaxLength: "5"}, "Between 0 and 5 characters."},
		{&spec.Schema{MinItems: "1", MaxItems: "2", UniqueItems: true}, "Between 1 and 2 items. Items are unique."},
		{&spec.Schema{MultipleOf: "5"}, "A multiple of 5."},
		{&spec.Schema{Pattern: "^a$"}, "Matches the pattern ^a$."},
		{&spec.Schema{Not: &spec.Schema{Types: []string{"null"}}}, "Must not be null."},
		{&spec.Schema{Not: &spec.Schema{}}, ""},
		{&spec.Schema{Deprecated: true, ReadOnly: true}, "Deprecated. Read-only: returned by the API, ignored in requests."},
		{&spec.Schema{WriteOnly: true}, "Write-only: accepted in requests, never returned."},
		{&spec.Schema{Enum: []*yaml.Node{n("1"), n("2")}, Default: n("1"), Example: n("2")}, "Allowed values: 1, 2. Default: 1. Example: 2."},
	}
	for _, c := range cases {
		if got := strings.Join(details(c.s), " "); got != c.want {
			t.Errorf("details(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}

func TestStatusText(t *testing.T) {
	cases := map[string]string{
		"200": "200 OK", "201": "201 Created", "418": "418 I'm a teapot", "299": "299",
		"default": "Any other", "1XX": "1XX Informational", "3xx": "3xx Redirection",
		"5XX": "5XX Server error", "6XX": "6XX", "abc": "abc",
	}
	for in, want := range cases {
		if got := statusText(in); got != want {
			t.Errorf("statusText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnchor(t *testing.T) {
	used := map[string]bool{}
	long := strings.Repeat("x", 50)
	cases := []struct{ name, want string }{
		{"Pet", "Model_Pet"},
		{"Pet", "Model_Pet_2"},
		{"a.b-c d", "Model_a_b_c_d"},
		{long, "Model_" + strings.Repeat("x", 34)},
		{long, "Model_" + strings.Repeat("x", 32) + "_2"},
		{"\u00e9", "Model__"},
	}
	for _, c := range cases {
		if got := anchor("Model_", c.name, used); got != c.want {
			t.Errorf("anchor(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// FuzzRender holds the two promises the tool makes about any input: Parse
// either accepts it or refuses it with an error, and anything accepted
// renders to a package whose every part is well-formed XML.
func FuzzRender(f *testing.F) {
	for _, name := range []string{"aur.json", "petstore3.json", "features.yaml"} {
		src, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(src)
	}
	f.Add([]byte(head + "components: {schemas: {A: {properties: {a: {$ref: '#/components/schemas/A'}}}}}"))
	f.Add([]byte(head + "x: &a [*a]"))
	f.Fuzz(func(t *testing.T, src []byte) {
		s, err := spec.Parse(src, "", nil)
		if err != nil {
			return
		}
		data, err := Render(s, docx.Letter)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		wellFormed(t, data)
	})
}
