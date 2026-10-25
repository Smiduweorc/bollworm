package proto

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/spec"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

const fixtures = "../../testdata/proto"

func compileFile(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	set, err := Compile(name, src, []string{fixtures})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return set
}

func mustWrite(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func parseSet(t *testing.T, set []byte) *spec.Spec {
	t.Helper()
	s, err := Parse(set)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func library(t *testing.T) *spec.Spec {
	return parseSet(t, compileFile(t, "library/v1/library.proto"))
}

// source compiles a.proto, written with files into a fresh import path.
func source(t *testing.T, files map[string]string) (*spec.Spec, error) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(dir, name), []byte(src))
	}
	set, err := Compile("a.proto", []byte(files["a.proto"]), []string{dir})
	if err != nil {
		return nil, err
	}
	return Parse(set)
}

func model(t *testing.T, s *spec.Spec, name string) *spec.Schema {
	t.Helper()
	for _, m := range s.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no model %q", name)
	return nil
}

func field(t *testing.T, m *spec.Schema, name string) *spec.Property {
	t.Helper()
	for _, p := range m.Properties {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("%s has no field %q", m.Name, name)
	return nil
}

func rpc(t *testing.T, s *spec.Spec, service, method string) *spec.Operation {
	t.Helper()
	for _, g := range s.Groups {
		for _, op := range g.Operations {
			if g.Name == service && op.ID == method {
				return op
			}
		}
	}
	t.Fatalf("no rpc %s.%s", service, method)
	return nil
}

func TestPackageAndFilesMakeTheTitleAndIntroduction(t *testing.T) {
	s := library(t)
	if s.Format != spec.Protobuf || s.Version != "proto3" || s.Title != "library.v1" || s.APIVersion != "v1" {
		t.Errorf("format %v, version %q, title %q, api version %q", s.Format, s.Version, s.Title, s.APIVersion)
	}
	want := "The lending library's catalogue: shelves, the books on them, and the\nreaders who borrow them.\n\nBook names are paths, `shelves/{shelf}/books/{book}`."
	if s.Description != want {
		t.Errorf("description %q", s.Description)
	}
	if !slices.Equal(s.Files, []spec.File{{Name: "library/v1/library.proto", Package: "library.v1"}}) {
		t.Errorf("only the compiled file is described, not its imports: %+v", s.Files)
	}
}

func TestVersionSuffixOfThePackage(t *testing.T) {
	cases := map[string]string{"library.v1": "v1", "acme.ads.v2beta3": "v2beta3", "x.v1alpha": "v1alpha", "library": "", "x.v1x": "", "x.version": ""}
	for pkg, want := range cases {
		s, err := source(t, map[string]string{"a.proto": "syntax = \"proto3\";\npackage " + pkg + ";\n"})
		if err != nil {
			t.Fatal(err)
		}
		if s.Title != pkg || s.APIVersion != want {
			t.Errorf("%s: title %q, version %q, want %q", pkg, s.Title, s.APIVersion, want)
		}
	}
}

func TestEachServiceIsAGroupInDeclarationOrder(t *testing.T) {
	s := library(t)
	var got []string
	for _, g := range s.Groups {
		for _, op := range g.Operations {
			got = append(got, g.Name+"."+op.ID)
		}
	}
	want := []string{"Library.GetBook", "Library.ListBooks", "Library.ImportBooks", "Library.UpdateBook", "Library.GetPrice", "Library.DeleteBook", "ReadingGroup.Discuss", "ReadingGroup.ListBooks"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
	if s.Groups[0].Description != "Reads and changes the catalogue." {
		t.Errorf("service comment: %q", s.Groups[0].Description)
	}
}

func TestStreamingKinds(t *testing.T) {
	s := library(t)
	cases := []struct {
		service, method string
		want            spec.Streaming
	}{
		{"Library", "GetBook", spec.Unary},
		{"Library", "ListBooks", spec.ServerStreaming},
		{"Library", "ImportBooks", spec.ClientStreaming},
		{"ReadingGroup", "Discuss", spec.BidiStreaming},
	}
	for _, c := range cases {
		if got := rpc(t, s, c.service, c.method).Streaming; got != c.want {
			t.Errorf("%s: %v, want %v", c.method, got, c.want)
		}
	}
}

func TestRPCCarriesItsMessagesAndPath(t *testing.T) {
	s := library(t)
	op := rpc(t, s, "Library", "ImportBooks")
	if op.Input != model(t, s, "Book") || op.Output != model(t, s, "ImportBooksResponse") {
		t.Errorf("input %+v, output %+v", op.Input, op.Output)
	}
	if op.RPC.Service != "library.v1.Library" || op.RPC.Method != "ImportBooks" || op.HTTP != nil {
		t.Errorf("rpc %+v, http %+v", op.RPC, op.HTTP)
	}
	if !strings.HasPrefix(op.Description, "Takes a stream of books, files each one, and reports how many it\ntook") {
		t.Errorf("description %q", op.Description)
	}
	del := rpc(t, s, "Library", "DeleteBook")
	if !del.Deprecated || rpc(t, s, "Library", "GetBook").Deprecated {
		t.Error("deprecated is read from the method's options")
	}
	if !slices.Equal(del.Output.Types, []string{"google.protobuf.Empty"}) || del.Output.Name != "" {
		t.Errorf("Empty is shown by name: %+v", del.Output)
	}
}

func TestHTTPAnnotationsBecomeBindings(t *testing.T) {
	s := library(t)
	cases := map[string][]spec.Binding{
		"GetBook": {{Method: "GET", Path: "/v1/{name=shelves/*/books/*}"}},
		"UpdateBook": {
			{Method: "PATCH", Path: "/v1/{book.name=shelves/*/books/*}", Body: "book"},
			{Method: "PUT", Path: "/v1/{book.name=shelves/*/books/*}", Body: "*"},
		},
		"GetPrice":    {{Method: "GET", Path: "/v1/{name=shelves/*/books/*}/price", ResponseBody: "price"}},
		"DeleteBook":  {{Method: "DELETE", Path: "/v1/{name=shelves/*/books/*}"}},
		"ImportBooks": nil,
	}
	for method, want := range cases {
		if got := rpc(t, s, "Library", method).RPC.Bindings; !slices.Equal(got, want) {
			t.Errorf("%s: %+v", method, got)
		}
	}
}

func TestCustomHTTPPattern(t *testing.T) {
	src := `syntax = "proto3";
package x;
import "google/api/annotations.proto";
service S {
  rpc Peek(M) returns (M) { option (google.api.http) = {custom: {kind: "head" path: "/v1/m"}}; }
}
message M {}
`
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.proto"), []byte(src))
	set, err := Compile("x.proto", []byte(src), []string{dir, fixtures})
	if err != nil {
		t.Fatal(err)
	}
	got := rpc(t, parseSet(t, set), "S", "Peek").RPC.Bindings
	if !slices.Equal(got, []spec.Binding{{Method: "HEAD", Path: "/v1/m"}}) {
		t.Errorf("%+v", got)
	}
}

func TestModelsAreDeclaredTypesThenImportedOnes(t *testing.T) {
	s := library(t)
	var got []string
	for _, m := range s.Models {
		got = append(got, m.Name)
	}
	want := []string{"Book", "Book.Edition", "Book.Format", "Shelf", "GetBookRequest", "ListBooksRequest", "ImportBooksResponse", "UpdateBookRequest", "Note", "Genre", "Money"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestFieldsOfAMessage(t *testing.T) {
	s := library(t)
	book := model(t, s, "Book")
	if book.Description != "A book in the catalogue." || len(book.Properties) != 15 {
		t.Fatalf("%q, %d fields", book.Description, len(book.Properties))
	}
	if p := field(t, book, "shelf_mark"); p.Number != 15 || !p.Schema.Deprecated || p.Schema.Description != "Use `labels` instead." {
		t.Errorf("shelf_mark: %d %+v", p.Number, p.Schema)
	}
	if p := field(t, book, "authors"); !slices.Equal(p.Schema.Types, []string{"array"}) || !slices.Equal(p.Schema.Items.Types, []string{"string"}) || p.Schema.Description != "In the order the title page lists them." {
		t.Errorf("repeated: %+v", p.Schema)
	}
	if p := field(t, book, "related"); p.Schema.Items != book {
		t.Error("a message that contains itself points back at its own model")
	}
	if p := field(t, book, "editions"); p.Schema.Items != model(t, s, "Book.Edition") {
		t.Error("a nested message is its own model")
	}
	if p := field(t, book, "price"); p.Schema != model(t, s, "Money") {
		t.Error("a field of a message type with no comment is the model itself")
	}
	if p := field(t, book, "format"); p.Schema != model(t, s, "Book.Format") {
		t.Error("an enum field is the enum's model")
	}
	if p := field(t, book, "published"); !slices.Equal(p.Schema.Types, []string{"google.protobuf.Timestamp"}) || p.Schema.Name != "" {
		t.Errorf("well-known types are shown by name: %+v", p.Schema)
	}
	lp := field(t, book, "loan_period").Schema
	if !slices.Equal(lp.Types, []string{"google.protobuf.Duration"}) || lp.Description != "How long a reader may keep it." {
		t.Errorf("loan_period: %+v", lp)
	}
}

func TestACommentedFieldOfAModelTypeWrapsTheModel(t *testing.T) {
	s, err := source(t, map[string]string{"a.proto": `syntax = "proto3";
package a;
message A {
  // The one it came from.
  B origin = 1 [deprecated = true];
}
message B {}
`})
	if err != nil {
		t.Fatal(err)
	}
	f := field(t, model(t, s, "A"), "origin").Schema
	b := model(t, s, "B")
	if len(f.AllOf) != 1 || f.AllOf[0] != b || f.Description != "The one it came from." || !f.Deprecated {
		t.Errorf("%+v", f)
	}
	if b.Description != "" || b.Deprecated {
		t.Error("the field's comment must not land on the shared model")
	}
}

func TestOneofAndOptional(t *testing.T) {
	book := model(t, library(t), "Book")
	for name, want := range map[string]string{"isbn": "identifier", "asin": "identifier", "pages": "", "title": ""} {
		if got := field(t, book, name).Oneof; got != want {
			t.Errorf("%s: oneof %q, want %q (proto3 optional is a synthetic oneof and is left out)", name, got, want)
		}
	}
	if got := field(t, book, "isbn").Schema.Description; got != "ISBN-13, digits only." {
		t.Errorf("a trailing comment is the description: %q", got)
	}
}

func TestMaps(t *testing.T) {
	s := library(t)
	labels := field(t, model(t, s, "Book"), "labels").Schema
	if !slices.Equal(labels.Key.Types, []string{"string"}) || !slices.Equal(labels.AdditionalProperties.Types, []string{"string"}) || labels.Description != "Free-form labels, such as `condition: worn`." {
		t.Errorf("labels: %+v", labels)
	}
	byPos := field(t, model(t, s, "Shelf"), "books_by_position").Schema
	if !slices.Equal(byPos.Key.Types, []string{"int32"}) || byPos.AdditionalProperties != model(t, s, "Book") {
		t.Errorf("books_by_position: %+v", byPos)
	}
	for _, m := range s.Models {
		if strings.HasSuffix(m.Name, "Entry") {
			t.Errorf("map entry %s is not a model", m.Name)
		}
	}
}

func TestEnumValuesKeepNumbersAndComments(t *testing.T) {
	g := model(t, library(t), "Genre")
	want := []spec.EnumValue{
		{Name: "GENRE_UNSPECIFIED"},
		{Name: "FICTION", Number: 1},
		{Name: "HISTORY", Number: 2},
		{Name: "BIOGRAPHY", Number: 3, Description: "Merged into HISTORY.", Deprecated: true},
		{Name: "POETRY", Number: 10},
	}
	if !slices.Equal(g.Values, want) || !slices.Equal(g.Types, []string{"enum"}) || g.Description != "The section of the library a shelf is in." {
		t.Errorf("%+v", g)
	}
}

func TestProto2RequiredAndGroups(t *testing.T) {
	s, err := source(t, map[string]string{"a.proto": `syntax = "proto2";
package a;
message A {
  required string id = 1;
  optional string note = 2;
  optional group Extra = 3 { optional int32 n = 4; }
}
`})
	if err != nil {
		t.Fatal(err)
	}
	a := model(t, s, "A")
	if s.Version != "proto2" || !field(t, a, "id").Required || field(t, a, "note").Required {
		t.Errorf("version %q, %+v", s.Version, a.Properties)
	}
	if extra := field(t, a, "extra").Schema; extra != model(t, s, "A.Extra") {
		t.Errorf("a group is a message: %+v", extra)
	}
}

func TestCommentsLoseTheSpaceAfterTheSlashes(t *testing.T) {
	s, err := source(t, map[string]string{"a.proto": `syntax = "proto3";
package a;
// First line.
//
//     indented code
/* block */
message A {}
`})
	if err != nil {
		t.Fatal(err)
	}
	// The block comment directly above is the leading comment; the line
	// comments above it are detached and left out.
	if got := model(t, s, "A").Description; got != "block" {
		t.Errorf("%q", got)
	}
	s, err = source(t, map[string]string{"a.proto": "syntax = \"proto3\";\npackage a;\n// First line.\n//\n//     indented code\nmessage A {}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got := model(t, s, "A").Description; got != "First line.\n\n    indented code" {
		t.Errorf("%q", got)
	}
}

func TestSetWithoutImportsShowsMissingTypesByName(t *testing.T) {
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(compileFile(t, "library/v1/library.proto"), &set); err != nil {
		t.Fatal(err)
	}
	set.File = set.File[len(set.File)-1:]
	if set.File[0].GetName() != "library/v1/library.proto" {
		t.Fatalf("the compiled file comes last, after its imports: %s", set.File[0].GetName())
	}
	data, _ := proto.Marshal(&set)
	s := parseSet(t, data)
	price := field(t, model(t, s, "Book"), "price").Schema
	if !slices.Equal(price.Types, []string{"library.v1.Money"}) || price.Name != "" {
		t.Errorf("price: %+v", price)
	}
	if got := rpc(t, s, "Library", "GetPrice").RPC.Bindings; len(got) != 1 || got[0].ResponseBody != "price" {
		t.Errorf("google.api.http is read without annotations.proto in the set: %+v", got)
	}
	for _, m := range s.Models {
		if m.Name == "Money" {
			t.Error("Money's file is not in the set, so it has no model")
		}
	}
}

func TestSeveralFilesInSeveralPackages(t *testing.T) {
	a := compileFile(t, "library/v1/library.proto")
	h := compileFile(t, "haberdasher/v1/haberdasher.proto")
	var sa, sh descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(a, &sa); err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(h, &sh); err != nil {
		t.Fatal(err)
	}
	sa.File = append(sa.File, sh.File...)
	data, _ := proto.Marshal(&sa)
	s := parseSet(t, data)
	if s.Title != "" || s.APIVersion != "" || len(s.Files) != 2 || s.Files[1].Name != "haberdasher/v1/haberdasher.proto" {
		t.Errorf("title %q, files %+v", s.Title, s.Files)
	}
	model(t, s, "library.v1.Book")
	model(t, s, "haberdasher.v1.Hat")
	if len(s.Groups) != 3 || s.Groups[2].Name != "Haberdasher" {
		t.Errorf("groups %+v", s.Groups)
	}
}

func TestSameDocumentFromSourceOrSet(t *testing.T) {
	set := compileFile(t, "haberdasher/v1/haberdasher.proto")
	s := parseSet(t, set)
	if s.Groups[0].Name != "Haberdasher" || rpc(t, s, "Haberdasher", "MakeHat").Input != model(t, s, "MakeHatRequest") {
		t.Errorf("%+v", s.Groups)
	}
	if got := field(t, model(t, s, "MakeHatRequest"), "centimetres").Schema.Description; got != "Head circumference in centimetres. Must be positive." {
		t.Errorf("%q", got)
	}
}

func TestCompileErrorsNameTheFileAndLine(t *testing.T) {
	cases := map[string]string{
		"syntax = \"proto3\";\npackage x;\nmessage A {\n  strin name = 1;\n}\n": "x.proto:4:3: ",
		"syntax = \"proto3\"\nmessage A {}\n":                                   "x.proto:2:1: syntax error",
		"syntax = \"proto3\";\nmessage A { int32 a = 1; int32 b = 1; }\n":       "x.proto:2:",
	}
	for src, want := range cases {
		_, err := Compile("x.proto", []byte(src), []string{t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", src, err, want)
		}
	}
}

func TestMissingImportIsNotFound(t *testing.T) {
	_, err := Compile("x.proto", []byte("syntax = \"proto3\";\nimport \"nope/missing.proto\";\n"), []string{t.TempDir()})
	if !errors.Is(err, protoregistry.NotFound) || !strings.Contains(err.Error(), `x.proto:2:8: `) || !strings.Contains(err.Error(), "nope/missing.proto") {
		t.Errorf("%v", err)
	}
}

func TestParseRefusesWhatIsNotADescriptorSet(t *testing.T) {
	cases := map[string]string{
		"":                 "holds no files",
		"\x0a\x03abc":      "not a FileDescriptorSet",
		"openapi: 3.1.0\n": "not a FileDescriptorSet",
	}
	for data, want := range cases {
		if _, err := Parse([]byte(data)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", data, err, want)
		}
	}
	twice, _ := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{Name: proto.String("a.proto")}, {Name: proto.String("a.proto")}}})
	if _, err := Parse(twice); err == nil || !strings.Contains(err.Error(), `"a.proto"`) {
		t.Errorf("a file twice: %v", err)
	}
}

func TestDetect(t *testing.T) {
	set := compileFile(t, "haberdasher/v1/haberdasher.proto")
	cases := []struct {
		name        string
		data        string
		set, source bool
	}{
		{"descriptor set", string(set), true, false},
		{"proto3", "syntax = \"proto3\";\n", false, true},
		{"no spaces", "syntax=\"proto2\";", false, true},
		{"edition", "edition = \"2023\";\n", false, true},
		{"after comments", "// Copyright\n/* licence\n * text */\n\nsyntax = \"proto3\";", false, true},
		{"yaml", "openapi: 3.1.0\ninfo:\n\ttitle: x\n", false, false},
		{"json", "{\"openapi\": \"3.1.0\"}\r\n", false, false},
		{"syntax as a yaml key", "syntax: proto3\n", false, false},
		{"syntax later in the file", "package x;\nsyntax = \"proto3\";", false, false},
		{"empty", "", false, false},
	}
	for _, c := range cases {
		if set, source := Detect([]byte(c.data)); set != c.set || source != c.source {
			t.Errorf("%s: set %v source %v", c.name, set, source)
		}
	}
}
