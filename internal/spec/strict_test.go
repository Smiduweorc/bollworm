package spec

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEveryErrorIsReported(t *testing.T) {
	_, err := Parse([]byte(`openapi: 3.1.0
paths:
  /a:
    get:
      responses: {"200": {$ref: '#/nope/a'}}
    post:
      responses: {"200": {$ref: '#/nope/b'}}
  /b:
    get:
      parameters: [{$ref: '#/nope/a'}]
components:
  schemas:
    A: {$ref: '#/nope/c'}
    B: 7
`), "", nil)
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("got %v", err)
	}
	var lines []int
	for _, e := range errs {
		lines = append(lines, e.Line)
	}
	if !slices.Equal(lines, []int{5, 7, 10, 13, 14}) {
		t.Errorf("lines %v:\n%v", lines, err)
	}
	// errors.As still finds the first refusal on its own.
	var first *Error
	if !errors.As(err, &first) || first.Line != 5 {
		t.Errorf("first: %v", first)
	}
}

func TestOperationsAreFiledUnderEveryTag(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a: {get: {operationId: a, tags: [One, Two, One], responses: {}}}
  /b: {get: {operationId: b, tags: [Three, Two], responses: {}}}
`)
	var names []string
	for _, g := range s.Groups {
		var ops, also []string
		for _, op := range g.Operations {
			ops = append(ops, op.ID)
		}
		for _, op := range g.Also {
			also = append(also, op.ID)
		}
		names = append(names, g.Name+":"+strings.Join(ops, ",")+"/"+strings.Join(also, ","))
	}
	if want := []string{"One:a/", "Two:/a,b", "Three:b/"}; !slices.Equal(names, want) {
		t.Errorf("got %q, want %q", names, want)
	}
}

func TestKeywordsBesideARefAreKept(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Pet: {type: object}
    Owner:
      properties:
        pet: {$ref: '#/components/schemas/Pet', readOnly: true, example: {a: 1}, description: Theirs, type: [object, "null"]}
`)
	pet := model(t, s, "Owner").Properties[0].Schema
	if len(pet.AllOf) != 1 || pet.AllOf[0] != model(t, s, "Pet") || !pet.ReadOnly || pet.Example == nil || pet.Description != "Theirs" || !slices.Contains(pet.Types, "null") {
		t.Errorf("%+v", pet)
	}
}

func TestAnchorsIDsAndDynamicRefs(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    Named: {$anchor: pet, type: string, title: by anchor}
    Old: {$id: '#legacy', type: integer}
    Remote:
      $id: 'https://example.com/schemas/remote'
      type: object
      properties:
        self: {$ref: '#inner'}
      $defs:
        Inner: {$anchor: inner, type: boolean}
    Tree: {$dynamicAnchor: node, type: object, properties: {kids: {type: array, items: {$dynamicRef: '#node'}}}}
    Uses:
      properties:
        a: {$ref: '#pet'}
        b: {$ref: '#legacy'}
        c: {$ref: 'https://example.com/schemas/remote'}
`)
	props := model(t, s, "Uses").Properties
	if props[0].Schema != model(t, s, "Named") || props[1].Schema != model(t, s, "Old") || props[2].Schema != model(t, s, "Remote") {
		t.Errorf("anchors: %+v %+v %+v", props[0].Schema, props[1].Schema, props[2].Schema)
	}
	// A $ref inside an $id resolves against that $id's document.
	if self := model(t, s, "Remote").Properties[0].Schema; !slices.Equal(self.Types, []string{"boolean"}) {
		t.Errorf("inner: %+v", self)
	}
	tree := model(t, s, "Tree")
	if tree.Properties[0].Schema.Items != tree {
		t.Error("$dynamicRef is followed as a $ref")
	}
}

func TestFalseClosedAndEveryExample(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
components:
  schemas:
    A:
      type: object
      additionalProperties: false
      properties: {gone: false, n: {type: integer, examples: [1, 2]}}
    B: {type: object, unevaluatedProperties: false}
    C: {type: string, example: x, examples: [y]}
`)
	a := model(t, s, "A")
	if !a.Closed || !model(t, s, "B").Closed || !a.Properties[0].Schema.Never {
		t.Errorf("closed %v / %v, never %v", a.Closed, model(t, s, "B").Closed, a.Properties[0].Schema.Never)
	}
	if n := a.Properties[1].Schema; len(n.Examples) != 2 || n.Example.Value != "1" {
		t.Errorf("examples: %+v", n.Examples)
	}
	if c := model(t, s, "C"); len(c.Examples) != 2 || c.Example.Value != "x" {
		t.Errorf("example then examples: %+v", c.Examples)
	}
}

func TestLinksFindTheirTarget(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          links:
            byID: {operationId: getB}
            byRef: {operationRef: '#/paths/~1b/get'}
            toCallback: {operationId: notify}
            nowhere: {operationId: missing}
      callbacks:
        cb:
          '{$url}': {post: {operationId: notify, responses: {}}}
  /b:
    get: {operationId: getB, responses: {}}
`)
	ep := s.Groups[0].Operations[0].HTTP
	links := ep.Responses[0].Links
	b := s.Groups[0].Operations[1]
	if links[0].Target != b || links[1].Target != b || links[2].Target != ep.Callbacks[0].Operations[0] || links[3].Target != nil {
		t.Errorf("targets: %v %v %v %v", links[0].Target, links[1].Target, links[2].Target, links[3].Target)
	}
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], `operationId "missing", which no operation has`) {
		t.Errorf("warnings %q", s.Warnings)
	}
}

func TestWarnings(t *testing.T) {
	s := parse(t, `openapi: 3.1.0
paths:
  /a:
    get:
      operationId: same
      parameters:
        - name: q
          in: query
          content: {application/json: {}, text/plain: {}}
      responses: {}
    post: {operationId: same, responses: {}}
components:
  schemas:
    P:
      oneOf: [{type: string}]
      discriminator: {propertyName: kind, mapping: {x: '#/components/schemas/Nope'}}
`)
	want := []string{
		`line 11: operationId "same" is used by more than one operation`,
		`line 9: parameter "q" has 2 media types under content`,
		`line 16: discriminator mapping "#/components/schemas/Nope" does not resolve`,
	}
	if len(s.Warnings) != len(want) {
		t.Fatalf("warnings %q", s.Warnings)
	}
	for _, w := range want {
		if !slices.ContainsFunc(s.Warnings, func(got string) bool { return strings.HasPrefix(got, w) }) {
			t.Errorf("missing %q in %q", w, s.Warnings)
		}
	}
}

func TestAnUnreachableDocumentIsReportedOnce(t *testing.T) {
	calls := 0
	_, err := Parse([]byte(`openapi: 3.1.0
components:
  schemas:
    A: {$ref: 'x.yaml#/A'}
    B: {$ref: 'x.yaml#/B'}
`), filepath.Join(t.TempDir(), "main.yaml"), func(string) ([]byte, error) { calls++; return nil, errors.New("gone") })
	var errs Errors
	if !errors.As(err, &errs) || len(errs) != 1 || calls != 1 {
		t.Errorf("%d calls: %v", calls, err)
	}
}
