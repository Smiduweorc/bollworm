package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hiveFile = "../../testdata/openrpc-features.yaml"

func TestOpenRPCFromAFileAndFromStdinAreTheSame(t *testing.T) {
	src, err := os.ReadFile(hiveFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(renderOK(t, "", hiveFile), renderOK(t, string(src), "-")) {
		t.Error("stdin and the file give different documents")
	}
	renderOK(t, "", "../../testdata/openrpc-petstore.json")
}

func TestOpenRPCProblemsExitOneWithTheReason(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	mustWrite(t, bad, []byte(`{"openrpc": "2.0.0", "methods": []}`))
	out := filepath.Join(dir, "out.docx")
	r := cli(t, "", "render", bad, "-o", out)
	if r.code != exitFailure || r.stderr != "bollworm: "+bad+": line 1: OpenRPC 2.0.0 is not supported; bollworm reads 1.x\n" {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused spec wrote a file")
	}
}

func TestOpenRPCRefusesProtoPath(t *testing.T) {
	r := cli(t, "", "render", hiveFile, "-I", protoRoot, "-o", filepath.Join(t.TempDir(), "out.docx"))
	if r.code != exitUsage || !strings.Contains(r.stderr, "--proto-path is for .proto input") {
		t.Errorf("%+v", r)
	}
}

func TestHelpDescribesOpenRPCInput(t *testing.T) {
	r := cli(t, "", "render", "--help")
	for _, want := range []string{"an OpenRPC 1.x\ndocument", "bollworm render openrpc.json -o api.docx"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("render help is missing %q", want)
		}
	}
}
