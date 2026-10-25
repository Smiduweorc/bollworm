package spec

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) *Spec {
	t.Helper()
	s, err := Parse([]byte(src), "", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

// refusal parses src, requires a *Error, and returns it.
func refusal(t *testing.T, src string) *Error {
	t.Helper()
	_, err := Parse([]byte(src), "", nil)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %v", err)
	}
	return e
}

func model(t *testing.T, s *Spec, name string) *Schema {
	t.Helper()
	for _, m := range s.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no model %q", name)
	return nil
}

func ops(s *Spec) []*Operation {
	var out []*Operation
	for _, g := range s.Groups {
		out = append(out, g.Operations...)
	}
	return out
}

func TestVersionsAccepted(t *testing.T) {
	for _, v := range []string{"3.0.0", "3.0.3", "3.1.0", "3.1.1", "3.2.0", "3.1"} {
		if _, err := Parse([]byte("openapi: "+v+"\ninfo: {title: t, version: '1'}\n"), "", nil); err != nil {
			t.Errorf("%s refused: %v", v, err)
		}
	}
}

func TestVersionsRefused(t *testing.T) {
	cases := []struct {
		src  string
		line int
		want string
	}{
		{"swagger: '2.0'\ninfo: {title: t}\n", 1, "Swagger 2.0 document"},
		{"info: {title: t}\n", 0, `no "openapi" or "openrpc" version field`},
		{"info: {title: t}\nopenapi: 4.0.0\n", 2, "OpenAPI 4.0.0 is not supported"},
		{"openapi: 3.10.0\n", 1, "OpenAPI 3.10.0 is not supported"},
		{"openapi: 2.0.0\n", 1, "OpenAPI 2.0.0 is not supported"},
	}
	for _, c := range cases {
		e := refusal(t, c.src)
		if e.Line != c.line || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%q: got line %d %q, want line %d containing %q", c.src, e.Line, e.Msg, c.line, c.want)
		}
	}
}

func TestMalformedDocumentsAreRefused(t *testing.T) {
	cases := map[string]string{
		"":                     "the document is empty",
		"- a\n- b\n":           "not a mapping of OpenAPI fields",
		"openapi: [3.1\n":      "did not find expected",
		"openapi: 3.1.0\n\t\n": "line 2: found a tab character",
	}
	for src, want := range cases {
		if e := refusal(t, src); !strings.Contains(e.Error(), want) {
			t.Errorf("%q: got %q, want it to contain %q", src, e.Error(), want)
		}
	}
}

func TestJSONIsRead(t *testing.T) {
	s := parse(t, `{"openapi": "3.0.3", "info": {"title": "J", "version": "9"}, "paths": {"/a": {"get": {"summary": "A", "responses": {"200": {"description": "ok"}}}}}}`)
	if s.Title != "J" || s.APIVersion != "9" || len(ops(s)) != 1 || ops(s)[0].HTTP.Responses[0].Status != "200" {
		t.Errorf("unexpected spec: %+v", s)
	}
}

func TestInfoFields(t *testing.T) {
	s := parse(t, `
openapi: 3.1.0
info:
  title: T
  version: "1.2"
  summary: S
  description: D
  termsOfService: https://example.com/tos
  contact: {name: N, url: https://example.com, email: n@example.com}
  license: {name: Apache 2.0, identifier: Apache-2.0}
servers:
  - url: https://{env}.example.com
    description: main
    variables:
      env: {default: prod, enum: [prod, test], description: which}
`)
	if s.Title != "T" || s.APIVersion != "1.2" || s.Summary != "S" || s.Description != "D" || s.TermsURL != "https://example.com/tos" {
		t.Errorf("info: %+v", s)
	}
	if s.Contact != (Contact{"N", "https://example.com", "n@example.com"}) {
		t.Errorf("contact: %+v", s.Contact)
	}
	if s.License != (License{"Apache 2.0", "https://spdx.org/licenses/Apache-2.0.html"}) {
		t.Errorf("license: %+v", s.License)
	}
	want := Server{URL: "https://{env}.example.com", Description: "main", Variables: []ServerVariable{{"env", "prod", "which", []string{"prod", "test"}}}}
	if len(s.Servers) != 1 || s.Servers[0].URL != want.URL || s.Servers[0].Description != want.Description ||
		len(s.Servers[0].Variables) != 1 || !slices.Equal(s.Servers[0].Variables[0].Enum, want.Variables[0].Enum) ||
		s.Servers[0].Variables[0].Default != "prod" {
		t.Errorf("servers: %+v", s.Servers)
	}
}

func TestLicenseURLWinsOverIdentifier(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\ninfo: {license: {name: X, url: https://example.com/x, identifier: MIT}}\n")
	if s.License.URL != "https://example.com/x" {
		t.Errorf("got %q", s.License.URL)
	}
}

func TestRefOutsideTheFileIsRefusedWithItsLine(t *testing.T) {
	e := refusal(t, `openapi: 3.1.0
paths:
  /a:
    get:
      responses:
        "200":
          $ref: other.yaml#/Response
`)
	if e.Line != 7 || !strings.Contains(e.Msg, `"other.yaml#/Response" points outside this file`) {
		t.Errorf("got %v", e)
	}
}

func TestRefsThatDoNotResolveAreRefused(t *testing.T) {
	cases := map[string]string{
		"#/components/schemas/Missing": `nothing at "Missing"`,
		"#Pet":                         `no $anchor named "Pet"`,
		"#/components/schemas/A/0":     `nothing at "0"`,
		"#/x%zz":                       "not a valid URI fragment",
	}
	for ref, want := range cases {
		e := refusal(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {type: string}\n    B: {$ref: '"+ref+"'}\n")
		if e.Line != 5 || !strings.Contains(e.Msg, want) {
			t.Errorf("%s: got %v, want line 5 containing %q", ref, e, want)
		}
	}
}

func TestRefLoopIsRefused(t *testing.T) {
	e := refusal(t, `openapi: 3.1.0
paths:
  /a:
    get:
      parameters:
        - $ref: '#/components/parameters/A'
components:
  parameters:
    A: {$ref: '#/components/parameters/B'}
    B: {$ref: '#/components/parameters/A'}
`)
	if !strings.Contains(e.Msg, "loop of references") {
		t.Errorf("got %v", e)
	}
}

func TestPointerEscapesAreDecoded(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: '#/x/a~1b/c~0d/with%20space/1'}
x:
  a/b:
    c~d:
      with space:
        - {type: string}
        - {type: integer}
`)
	got := ops(s)[0].HTTP.Responses[0].Content[0].Schema
	if !slices.Equal(got.Types, []string{"integer"}) {
		t.Errorf("got %+v", got)
	}
}

func TestRecursiveSchemaPointsBackAtItself(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Node:
      type: object
      properties:
        children:
          type: array
          items: {$ref: '#/components/schemas/Node'}
`)
	n := model(t, s, "Node")
	if n.Properties[0].Schema.Items != n {
		t.Error("children items should be the Node schema itself")
	}
}

func TestMutualRecursionResolves(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    A: {properties: {b: {$ref: '#/components/schemas/B'}}}
    B: {properties: {a: {$ref: '#/components/schemas/A'}}}
`)
	a, b := model(t, s, "A"), model(t, s, "B")
	if a.Properties[0].Schema != b || b.Properties[0].Schema != a {
		t.Error("A and B should point at each other")
	}
}

func TestRefWithSiblingsWrapsTheTarget(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Kind: {type: string, description: original}
    Item:
      properties:
        kind: {$ref: '#/components/schemas/Kind', description: overridden, deprecated: true}
        plain: {$ref: '#/components/schemas/Kind'}
`)
	kind := model(t, s, "Kind")
	item := model(t, s, "Item")
	w := item.Properties[0].Schema
	if w == kind || len(w.AllOf) != 1 || w.AllOf[0] != kind || w.Description != "overridden" || !w.Deprecated {
		t.Errorf("wrapper: %+v", w)
	}
	if kind.Description != "original" || kind.Deprecated {
		t.Error("the target must not change")
	}
	if item.Properties[1].Schema != kind {
		t.Error("a bare $ref is the target itself")
	}
}

func TestComponentSchemasAreNamedInDocumentOrder(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    Zebra: {type: string}\n    Apple: {type: string}\n")
	if len(s.Models) != 2 || s.Models[0].Name != "Zebra" || s.Models[1].Name != "Apple" {
		t.Errorf("models: %v, %v", s.Models[0].Name, s.Models[1].Name)
	}
}

func TestInlineSchemasHaveNoName(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {properties: {b: {type: object}}}\n")
	if model(t, s, "A").Properties[0].Schema.Name != "" {
		t.Error("an inline schema is not a model")
	}
}

func TestNullableAndTypeArraysReadTheSame(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    A: {type: string, nullable: true}
    B: {type: [string, "null"]}
    C: {type: [string, "null"], nullable: true}
    D: {type: string, nullable: false}
`)
	for name, want := range map[string][]string{"A": {"string", "null"}, "B": {"string", "null"}, "C": {"string", "null"}, "D": {"string"}} {
		if got := model(t, s, name).Types; !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}

func TestExclusiveBoundsInBothSpellings(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Old: {type: number, minimum: 0, exclusiveMinimum: true, maximum: 9, exclusiveMaximum: true}
    OldInclusive: {type: number, minimum: 0, exclusiveMinimum: false}
    New: {type: number, exclusiveMinimum: 0, exclusiveMaximum: 9}
`)
	for _, name := range []string{"Old", "New"} {
		m := model(t, s, name)
		if m.Minimum != "" || m.ExclusiveMinimum != "0" || m.Maximum != "" || m.ExclusiveMaximum != "9" {
			t.Errorf("%s: %+v", name, m)
		}
	}
	if m := model(t, s, "OldInclusive"); m.Minimum != "0" || m.ExclusiveMinimum != "" {
		t.Errorf("OldInclusive: %+v", m)
	}
}

func TestSchemaKeywords(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    A:
      title: Ti
      description: De
      type: string
      format: email
      enum: [a, b]
      const: a
      default: a
      example: b
      deprecated: true
      readOnly: true
      writeOnly: true
      minLength: 1
      maxLength: 9
      pattern: '^a'
      multipleOf: 2
    L: {type: array, items: {type: string}, minItems: 1, maxItems: 3, uniqueItems: true}
    E: {examples: [first, second]}
    M: {type: object, additionalProperties: {type: integer}}
    MAny: {type: object, additionalProperties: true}
    MNone: {type: object, additionalProperties: false}
    N: {not: {type: string}}
    T: true
`)
	a := model(t, s, "A")
	if a.Title != "Ti" || a.Description != "De" || a.Format != "email" || len(a.Enum) != 2 || a.Const.Value != "a" ||
		a.Default.Value != "a" || a.Example.Value != "b" || !a.Deprecated || !a.ReadOnly || !a.WriteOnly ||
		a.MinLength != "1" || a.MaxLength != "9" || a.Pattern != "^a" || a.MultipleOf != "2" {
		t.Errorf("A: %+v", a)
	}
	l := model(t, s, "L")
	if l.Items == nil || l.MinItems != "1" || l.MaxItems != "3" || !l.UniqueItems {
		t.Errorf("L: %+v", l)
	}
	if e := model(t, s, "E"); e.Example == nil || e.Example.Value != "first" {
		t.Error("the first of examples is the example")
	}
	if m := model(t, s, "M"); m.AdditionalProperties == nil || !slices.Equal(m.AdditionalProperties.Types, []string{"integer"}) {
		t.Error("M: map of integer")
	}
	if m := model(t, s, "MAny"); m.AdditionalProperties == nil || len(m.AdditionalProperties.Types) != 0 {
		t.Error("MAny: map of anything")
	}
	if m := model(t, s, "MNone"); m.AdditionalProperties != nil {
		t.Error("MNone: no map")
	}
	if n := model(t, s, "N"); n.Not == nil {
		t.Error("N: not")
	}
	if tr := model(t, s, "T"); len(tr.Types) != 0 || len(tr.Properties) != 0 {
		t.Error("T: accepts anything")
	}
}

func TestNonMappingSchemaIsRefused(t *testing.T) {
	e := refusal(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: [1, 2]\n")
	if e.Line != 4 || !strings.Contains(e.Msg, "a schema must be a mapping") {
		t.Errorf("got %v", e)
	}
}

func TestRequiredMarksProperties(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {required: [b], properties: {a: {}, b: {}}}\n")
	p := model(t, s, "A").Properties
	if p[0].Name != "a" || p[0].Required || p[1].Name != "b" || !p[1].Required {
		t.Errorf("properties: %+v %+v", p[0], p[1])
	}
}

func TestOperationsGroupByFirstTag(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
tags:
  - {name: Second, description: declared second}
  - {name: First}
  - {name: Second, description: duplicate}
  - {name: Empty}
paths:
  /a:
    get: {tags: [First, Second], summary: a}
  /b:
    get: {summary: untagged}
  /c:
    get: {tags: [Second], summary: c}
  /d:
    get: {tags: [Late], summary: d}
`)
	var names []string
	for _, g := range s.Groups {
		names = append(names, g.Name)
	}
	if !slices.Equal(names, []string{"Second", "First", "Late", ""}) {
		t.Fatalf("groups: %q", names)
	}
	if s.Groups[0].Description != "declared second" {
		t.Errorf("the first declaration of a tag wins: %q", s.Groups[0].Description)
	}
	if s.Groups[0].Operations[0].Summary != "c" || s.Groups[1].Operations[0].Summary != "a" || s.Groups[3].Operations[0].Summary != "untagged" {
		t.Error("operations filed under the wrong group")
	}
}

func TestNoUntaggedGroupWhenEverythingIsTagged(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\npaths:\n  /a:\n    get: {tags: [T]}\n")
	if len(s.Groups) != 1 || s.Groups[0].Name != "T" {
		t.Errorf("groups: %+v", s.Groups)
	}
}

func TestMethodsInFixedOrder(t *testing.T) {
	s := parse(t, `openapi: 3.2.0
paths:
  /a:
    additionalOperations:
      PURGE: {summary: purge}
    query: {summary: query}
    trace: {summary: trace}
    delete: {summary: delete}
    post: {summary: post}
    get: {summary: get}
    x-ignored: {summary: nope}
`)
	var got []string
	for _, op := range ops(s) {
		got = append(got, op.HTTP.Method)
	}
	if !slices.Equal(got, []string{"GET", "POST", "DELETE", "TRACE", "QUERY", "PURGE"}) {
		t.Errorf("got %v", got)
	}
}

func TestPathParametersMergeWithOperationParameters(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a/{id}:
    parameters:
      - {name: id, in: path, required: true, description: shared id}
      - {name: q, in: query, description: shared q}
      - {name: q, in: header, description: header q}
    get:
      parameters:
        - {name: q, in: query, description: own q}
        - {name: extra, in: query}
    post: {}
`)
	get, post := ops(s)[0], ops(s)[1]
	var got []string
	for _, p := range get.HTTP.Parameters {
		got = append(got, p.In+":"+p.Name+":"+p.Description)
	}
	want := []string{"path:id:shared id", "header:q:header q", "query:q:own q", "query:extra:"}
	if !slices.Equal(got, want) {
		t.Errorf("get: got %v, want %v", got, want)
	}
	if len(post.HTTP.Parameters) != 3 {
		t.Errorf("post inherits all three shared parameters, got %d", len(post.HTTP.Parameters))
	}
}

func TestParameterFields(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a:
    get:
      parameters:
        - {name: a, in: query, required: true, deprecated: true, schema: {type: integer}, example: 3}
        - name: b
          in: query
          content:
            application/json:
              schema: {type: object}
        - {name: c, in: cookie}
`)
	p := ops(s)[0].HTTP.Parameters
	if !p[0].Required || !p[0].Deprecated || p[0].Example.Value != "3" || !slices.Equal(p[0].Schema.Types, []string{"integer"}) {
		t.Errorf("a: %+v", p[0])
	}
	if p[1].Schema == nil || !slices.Equal(p[1].Schema.Types, []string{"object"}) {
		t.Errorf("b takes its schema from content: %+v", p[1].Schema)
	}
	if p[2].Schema != nil {
		t.Error("c has no schema")
	}
}

func TestSecurityInheritance(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
security:
  - key: []
paths:
  /inherit:
    get: {}
  /none:
    get: {security: []}
  /own:
    get:
      security:
        - oauth: [read, write]
          key: []
        - {}
webhooks:
  hook:
    post: {}
  securedHook:
    post: {security: [{key: []}]}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: K}
`)
	o := ops(s)
	if len(o[0].HTTP.Security) != 1 || o[0].HTTP.Security[0][0].Name != "key" {
		t.Errorf("inherit: %+v", o[0].HTTP.Security)
	}
	if o[1].HTTP.Security == nil || len(o[1].HTTP.Security) != 0 {
		t.Errorf("security: [] is an explicit empty requirement, got %#v", o[1].HTTP.Security)
	}
	own := o[2].HTTP.Security
	if len(own) != 2 || len(own[0]) != 2 || own[0][0].Name != "oauth" || !slices.Equal(own[0][0].Scopes, []string{"read", "write"}) || len(own[1]) != 0 {
		t.Errorf("own: %+v", own)
	}
	if s.Webhooks[0].HTTP.Security != nil {
		t.Error("a webhook does not inherit the document's security")
	}
	if len(s.Webhooks[1].HTTP.Security) != 1 {
		t.Error("a webhook keeps its own security")
	}
}

func TestSecurityMustBeAList(t *testing.T) {
	e := refusal(t, "openapi: 3.1.0\nsecurity: {key: []}\n")
	if e.Line != 2 || !strings.Contains(e.Msg, "security must be a list") {
		t.Errorf("got %v", e)
	}
}

func TestSecuritySchemes(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  securitySchemes:
    key: {type: apiKey, in: query, name: api_key, description: d}
    bearer: {type: http, scheme: bearer, bearerFormat: JWT}
    oauth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://example.com/token
          refreshUrl: https://example.com/refresh
          scopes: {read: Read things}
    ref: {$ref: '#/components/securitySchemes/key'}
`)
	if len(s.Schemes) != 4 {
		t.Fatalf("schemes: %d", len(s.Schemes))
	}
	k := s.Schemes[0]
	if k.Name != "key" || k.Type != "apiKey" || k.In != "query" || k.ParamName != "api_key" || k.Description != "d" {
		t.Errorf("key: %+v", k)
	}
	if b := s.Schemes[1]; b.HTTPScheme != "bearer" || b.BearerFormat != "JWT" {
		t.Errorf("bearer: %+v", b)
	}
	f := s.Schemes[2].Flows
	if len(f) != 1 || f[0].Kind != "clientCredentials" || f[0].TokenURL != "https://example.com/token" || f[0].RefreshURL != "https://example.com/refresh" || f[0].Scopes[0] != (Scope{"read", "Read things"}) {
		t.Errorf("flows: %+v", f)
	}
	if r := s.Schemes[3]; r.Name != "ref" || r.ParamName != "api_key" {
		t.Errorf("a referenced scheme keeps its own name: %+v", r)
	}
}

func TestBodiesResponsesAndExamples(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a:
    post:
      requestBody: {$ref: '#/components/requestBodies/B'}
      responses:
        "201":
          description: made
          headers:
            Location: {description: where, required: true, schema: {type: string}}
            Ref: {$ref: '#/components/headers/H'}
          content:
            application/json:
              example: {id: 1}
              examples:
                named: {summary: A summary, value: {id: 2}}
                bare: {value: {id: 3}}
                far: {externalValue: https://example.com/x.json}
                shared: {$ref: '#/components/examples/E'}
components:
  requestBodies:
    B: {description: body, required: true, content: {text/plain: {schema: {type: string}}}}
  headers:
    H: {schema: {type: integer}}
  examples:
    E: {summary: Shared, value: 4}
`)
	op := ops(s)[0]
	if b := op.HTTP.RequestBody; b == nil || !b.Required || b.Description != "body" || b.Content[0].Type != "text/plain" {
		t.Errorf("body: %+v", op.HTTP.RequestBody)
	}
	r := op.HTTP.Responses[0]
	if r.Status != "201" || r.Description != "made" {
		t.Errorf("response: %+v", r)
	}
	if h := r.Headers; len(h) != 2 || h[0].Name != "Location" || !h[0].Required || h[0].Description != "where" || h[1].Name != "Ref" || h[1].Schema == nil {
		t.Errorf("headers: %+v", h)
	}
	ex := r.Content[0].Examples
	var got []string
	for _, e := range ex {
		v := e.External
		if e.Value != nil {
			v = e.Value.Value
			if v == "" {
				v = e.Value.Content[1].Value
			}
		}
		got = append(got, e.Summary+"="+v)
	}
	want := []string{"=1", "A summary=2", "bare=3", "far=https://example.com/x.json", "Shared=4"}
	if !slices.Equal(got, want) {
		t.Errorf("examples: got %v, want %v", got, want)
	}
}

func TestAliasesAreFollowed(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
x-shared: &resp {description: shared response}
paths:
  /a:
    get:
      responses:
        "200": *resp
        "404": *resp
`)
	r := ops(s)[0].HTTP.Responses
	if len(r) != 2 || r[0].Description != "shared response" || r[1].Description != "shared response" {
		t.Errorf("responses: %+v %+v", r[0], r[1])
	}
}

func TestAllOfOneOfAnyOf(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Base: {properties: {a: {}}}
    X:
      allOf: [{$ref: '#/components/schemas/Base'}, {properties: {b: {}}}]
      oneOf: [{type: string}, {type: integer}]
      anyOf: [{type: boolean}]
`)
	x := model(t, s, "X")
	if len(x.AllOf) != 2 || x.AllOf[0] != model(t, s, "Base") || len(x.OneOf) != 2 || len(x.AnyOf) != 1 {
		t.Errorf("X: %+v", x)
	}
}

func TestEmptyPathsAndComponents(t *testing.T) {
	s := parse(t, "openapi: 3.1.0\npaths: {}\ncomponents: {}\n")
	if len(s.Groups) != 0 || len(s.Models) != 0 || len(s.Schemes) != 0 || s.Security != nil {
		t.Errorf("spec: %+v", s)
	}
}

func TestRealDocumentsParse(t *testing.T) {
	for _, name := range []string{"aur.json", "petstore3.json", "features.yaml"} {
		src, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(src, "", nil); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAURDocumentShape(t *testing.T) {
	src, err := os.ReadFile("../../testdata/aur.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse(src, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Counted by hand from testdata/aur.json.
	if s.Title != "AUR /rpc" || len(s.Groups) != 2 || len(s.Groups[0].Operations) != 3 || len(s.Groups[1].Operations) != 3 || len(s.Models) != 7 {
		t.Errorf("title %q, %d groups, %d models", s.Title, len(s.Groups), len(s.Models))
	}
	search := s.Groups[0].Operations[0]
	if search.HTTP.Path != "/rpc/v5/search/{arg}" || len(search.HTTP.Parameters) != 2 || search.HTTP.Parameters[1].Name != "by" || len(search.HTTP.Parameters[1].Schema.Enum) != 14 {
		t.Errorf("search: %+v", search)
	}
}
