package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/proto"
)

const (
	protoRoot   = "../../testdata/proto"
	libraryFile = protoRoot + "/library/v1/library.proto"
	hatFile     = protoRoot + "/haberdasher/v1/haberdasher.proto"
)

func renderOK(t *testing.T, stdin string, args ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.docx")
	r := cli(t, stdin, append(append([]string{"render"}, args...), "-o", out)...)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("%v: %+v", args, r)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	isDocx(t, data)
	return data
}

// librarySet is library.proto compiled to a FileDescriptorSet, as buf
// build or protoc --include_imports --include_source_info would write it.
func librarySet(t *testing.T) []byte {
	t.Helper()
	src, _ := os.ReadFile(libraryFile)
	set, err := proto.Compile("library/v1/library.proto", src, []string{protoRoot})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestProtoSourceWithProtoPath(t *testing.T) {
	long := renderOK(t, "", libraryFile, "--proto-path", protoRoot)
	short := renderOK(t, "", libraryFile, "-I", "../../testdata/nowhere", "-I", protoRoot)
	if !bytes.Equal(long, short) {
		t.Error("-I is --proto-path, and import paths are searched in order")
	}
}

func TestProtoImportPathDefaultsToTheFilesDirectory(t *testing.T) {
	renderOK(t, "", hatFile)
	r := cli(t, "", "render", libraryFile, "-o", filepath.Join(t.TempDir(), "x.docx"))
	if r.code != exitFailure || !strings.Contains(r.stderr, `library.proto:9:8: could not resolve path "google/api/annotations.proto"`) || !strings.Contains(r.stderr, "add the directory that holds it with --proto-path") {
		t.Errorf("%+v", r)
	}
}

func TestDescriptorSetGivesTheSameDocumentAsItsSource(t *testing.T) {
	fromSource := renderOK(t, "", libraryFile, "-I", protoRoot)
	dir := t.TempDir()
	for _, name := range []string{"api.binpb", "api.pb", "api.desc", "api.protoset", "api.bin", "api"} {
		path := filepath.Join(dir, name)
		mustWrite(t, path, librarySet(t))
		if got := renderOK(t, "", path); !bytes.Equal(got, fromSource) {
			t.Errorf("%s: a different document from the .proto", name)
		}
	}
	if got := renderOK(t, string(librarySet(t)), "-"); !bytes.Equal(got, fromSource) {
		t.Error("a descriptor set on stdin is recognised by its content")
	}
}

func TestProtoSourceIsRecognisedByContent(t *testing.T) {
	src, _ := os.ReadFile(hatFile)
	renderOK(t, string(src), "-")
	txt := filepath.Join(t.TempDir(), "hats.txt")
	mustWrite(t, txt, src)
	renderOK(t, "", txt)
}

func TestYAMLAndJSONExtensionsAreAlwaysOpenAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd.yaml")
	mustWrite(t, path, []byte("openapi: 3.1.0\ninfo: {title: \"bell \x07\", version: '1'}\n"))
	r := cli(t, "", "render", path, "-o", filepath.Join(t.TempDir(), "x.docx"))
	if r.code != exitFailure || !strings.Contains(r.stderr, "odd.yaml: ") || strings.Contains(r.stderr, "FileDescriptorSet") {
		t.Errorf("a control character in a .yaml is a YAML problem, not a descriptor set: %+v", r)
	}
}

func TestProtoPathIsOnlyForProtoSource(t *testing.T) {
	dir := t.TempDir()
	set := filepath.Join(dir, "api.binpb")
	mustWrite(t, set, librarySet(t))
	for _, in := range []string{aur, set} {
		r := cli(t, "", "render", in, "-I", protoRoot, "-o", filepath.Join(dir, "x.docx"))
		if r.code != exitUsage || !strings.Contains(r.stderr, "--proto-path is for .proto input, and "+in+" is not a .proto file") || !strings.Contains(r.stderr, "--help") {
			t.Errorf("%s: %+v", in, r)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x.docx")); err == nil {
		t.Error("a usage error wrote a file")
	}
}

func TestProtoProblemsExitOneWithTheReason(t *testing.T) {
	dir := t.TempDir()
	typo := filepath.Join(dir, "typo.proto")
	mustWrite(t, typo, []byte("syntax = \"proto3\";\npackage x;\nmessage A {\n  strin name = 1;\n}\n"))
	junk := filepath.Join(dir, "junk.binpb")
	mustWrite(t, junk, []byte("\x0a\x03abc"))
	cases := []struct {
		args []string
		in   string
		want string
	}{
		{[]string{typo}, "", "bollworm: typo.proto:4:3: field x.A.name: unknown type strin\n"},
		{[]string{"-"}, "syntax = \"proto3\";\nmessage A { strin x = 1; }\n", "bollworm: stdin:2:13: field A.x: unknown type strin\n"},
		{[]string{typo, "-I", protoRoot}, "", typo + " is not under any --proto-path"},
		{[]string{junk}, "", junk + ": not a FileDescriptorSet: "},
	}
	for _, c := range cases {
		out := filepath.Join(dir, "out.docx")
		r := cli(t, c.in, append(append([]string{"render"}, c.args...), "-o", out)...)
		if r.code != exitFailure || r.stdout != "" || !strings.Contains(r.stderr, c.want) || strings.Contains(r.stderr, "--help") {
			t.Errorf("%v: %+v", c.args, r)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%v wrote a file", c.args)
		}
	}
}

func TestHelpDescribesProtoInput(t *testing.T) {
	r := cli(t, "", "render", "--help")
	for _, want := range []string{"-I, --proto-path stringArray", "FileDescriptorSet", "--include_source_info", "bollworm render proto/library/v1/library.proto --proto-path proto -o library.docx"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("render help is missing %q", want)
		}
	}
}
