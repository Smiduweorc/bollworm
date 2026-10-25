package render

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/proto"
	"github.com/Smiduweorc/bollworm/internal/spec"
)

func protoSpec(t *testing.T, name, src string, importPaths ...string) *spec.Spec {
	t.Helper()
	set, err := proto.Compile(name, []byte(src), importPaths)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	s, err := proto.Parse(set, false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func protoDocument(t *testing.T, s *spec.Spec) string {
	t.Helper()
	data, _, err := Render(s, Options{Paper: docx.A4})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return wellFormed(t, data)
}

func libraryDocument(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../testdata/proto/library/v1/library.proto")
	if err != nil {
		t.Fatal(err)
	}
	return protoDocument(t, protoSpec(t, "library/v1/library.proto", string(src), "../../testdata/proto"))
}

// section is the visible text from the heading start up to the next of the
// given headings. Every heading is in the contents first, so the body's is
// the last one.
func section(text, start string, next ...string) string {
	i := strings.LastIndex(text, "\n"+start+"\n")
	if i < 0 {
		return ""
	}
	rest := text[i+1:]
	end := len(rest)
	for _, n := range next {
		if j := strings.Index(rest, "\n"+n+"\n"); j >= 0 && j+1 < end {
			end = j + 1
		}
	}
	return rest[:end]
}

func TestProtoTitlePageAndIntroduction(t *testing.T) {
	text := visible(libraryDocument(t))
	if !strings.HasPrefix(text, "library.v1\nAPI reference, version v1\nContents\n") {
		t.Errorf("title page: %q", text[:min(80, len(text))])
	}
	mustContain(t, text,
		"Introduction\nThe lending library's catalogue: shelves, the books on them, and the readers who borrow them.\nBook names are paths, shelves/{shelf}/books/{book}.\nFiles\n",
		"File | Package | \nlibrary/v1/library.proto | library.v1 | \n",
	)
}

func TestRPCSections(t *testing.T) {
	text := visible(libraryDocument(t))
	get := section(text, "GetBook", "ListBooks (Library)")
	mustContain(t, get,
		"RPC  /library.v1.Library/GetBook\nReturns one book.\nRequest: GetBookRequest.\nResponse: Book.\nStreaming: none. The client sends one request and gets one response.\nHTTP mapping\n",
		"Method | Path | Request body | Response body | \nGET | /v1/{name=shelves/*/books/*} | None; fields not in the path go in the query string | The whole response | \n",
	)
	mustContain(t, section(text, "ListBooks (Library)", "ImportBooks"),
		"Request: ListBooksRequest.\nResponse: a stream of Book.\nStreaming: server. The client sends one request and reads a stream of responses.\n")
	imp := section(text, "ImportBooks", "UpdateBook")
	mustContain(t, imp, "Request: a stream of Book.\nResponse: ImportBooksResponse.\nStreaming: client. The client sends a stream of requests and gets one response.\n")
	if strings.Contains(imp, "HTTP mapping") {
		t.Error("an rpc with no google.api.http has no mapping table")
	}
	mustContain(t, section(text, "Discuss", "ListBooks (ReadingGroup)"),
		"RPC  /library.v1.ReadingGroup/Discuss\n",
		"Request: a stream of Note.\nResponse: a stream of Note.\nStreaming: bidirectional. The client and the server each send a stream, independently of each other.\n")
	mustContain(t, section(text, "UpdateBook", "GetPrice"),
		"PATCH | /v1/{book.name=shelves/*/books/*} | book | The whole response | \nPUT | /v1/{book.name=shelves/*/books/*} | The whole request | The whole response | \n")
	mustContain(t, section(text, "GetPrice", "DeleteBook"), "GET | /v1/{name=shelves/*/books/*}/price | None; fields not in the path go in the query string | price | \n")
	mustContain(t, section(text, "DeleteBook", "ReadingGroup"),
		"RPC  /library.v1.Library/DeleteBook\nDeprecated. This method is still served but may be removed; avoid it in new code.\nRemoves a book.",
		"Response: google.protobuf.Empty.\n")
}

func TestRPCsWithOneNameInTwoServicesAreToldApart(t *testing.T) {
	text := visible(libraryDocument(t))
	for _, want := range []string{"\nListBooks (Library)\n", "\nListBooks (ReadingGroup)\n", "\nGetBook\n"} {
		if strings.Count(text, want) != 2 {
			t.Errorf("%q should appear in the contents and as a heading", want)
		}
	}
}

func TestProtoDataModels(t *testing.T) {
	text := visible(libraryDocument(t))
	mustContain(t, text, "Data models\nThese are the messages and enums that requests and responses carry. Types from google.protobuf are the well-known types and are shown by name.\n")
	mustContain(t, section(text, "Book", "Book.Edition"),
		"A book in the catalogue.\nField | Number | Type | Description | \n",
		"authors | 3 | repeated string | In the order the title page lists them. | \n",
		"format | 4 | Book.Format |  | \n",
		"isbn | 5 | string | ISBN-13, digits only.\nPart of oneof identifier: setting it clears the other fields of identifier. | \n",
		"asin | 6 | string | Part of oneof identifier: setting it clears the other fields of identifier. | \n",
		"labels | 7 | map<string, string> | Free-form labels, such as condition: worn. | \n",
		"published | 8 | google.protobuf.Timestamp |  | \n",
		"related | 12 | repeated Book | Books shelved next to this one. | \n",
		"pages | 13 | int32 | Unset when the page count is not known. | \n",
		"price | 14 | Money |  | \n",
		"shelf_mark | 15 | string | Use labels instead.\nDeprecated. | \n",
	)
	mustContain(t, section(text, "Shelf", "GetBookRequest"), "books_by_position | 3 | map<int32, Book> | Keyed by position on the shelf, from 1 at the left. | \n")
	mustContain(t, section(text, "Genre", "Money"),
		"Value | Number | Description | \nGENRE_UNSPECIFIED | 0 |  | \nFICTION | 1 |  | \nHISTORY | 2 |  | \nBIOGRAPHY | 3 | Merged into HISTORY.\nDeprecated. | \nPOETRY | 10 |  | \n")
}

func TestProtoTypesLinkToTheirModels(t *testing.T) {
	doc := libraryDocument(t)
	linksResolve(t, "library.proto", doc)
	for _, want := range []string{`w:anchor="Model_Book"`, `w:anchor="Model_Book_Format"`, `w:anchor="Model_Money"`, `w:anchor="Model_GetBookRequest"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing link %s", want)
		}
	}
	if strings.Contains(doc, `w:name="Model_google`) {
		t.Error("well-known types have no section")
	}
}

func TestProtoEdgeCases(t *testing.T) {
	s := protoSpec(t, "a.proto", `syntax = "proto2";
message Empty {}
message A {
  required string id = 1;
  // Old.
  optional Empty e = 2 [deprecated = true];
}
`)
	text := visible(protoDocument(t, s))
	if !strings.HasPrefix(text, "API reference\nAPI reference\n") {
		t.Errorf("a file with no package falls back to the default title: %q", text[:30])
	}
	mustContain(t, text,
		"Empty\nThis message has no fields.\n",
		"id | 1 | string | Required. | \n",
		"e | 2 | Empty | Old.\nDeprecated. | \n",
	)
	if strings.Contains(text, "Endpoints") {
		t.Error("a file with no services has no endpoint chapter")
	}
}

func TestProtoOutputIsDeterministic(t *testing.T) {
	src, _ := os.ReadFile("../../testdata/proto/library/v1/library.proto")
	a, _, _ := Render(protoSpec(t, "library/v1/library.proto", string(src), "../../testdata/proto"), Options{Paper: docx.A4})
	b, _, _ := Render(protoSpec(t, "library/v1/library.proto", string(src), "../../testdata/proto"), Options{Paper: docx.A4})
	if !bytes.Equal(a, b) {
		t.Error("the same .proto rendered twice gave different bytes")
	}
}
