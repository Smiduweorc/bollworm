package render

import (
	"os"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/proto"
)

const protoFixtures = "../../testdata/proto"

// FuzzProtoSource holds the .proto path to the same promise FuzzRender holds
// OpenAPI and OpenRPC to: compile, parse and render either refuse the input
// with an error or produce a well-formed package. Detect runs first, as in
// the CLI, and must not panic on anything.
func FuzzProtoSource(f *testing.F) {
	for _, name := range []string{"haberdasher/v1/haberdasher.proto", "library/v1/library.proto", "library/v1/common.proto"} {
		src, err := os.ReadFile(protoFixtures + "/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(src)
	}
	f.Add([]byte("syntax = \"proto3\";\nmessage A { A a = 1; repeated A b = 2; map<string, A> c = 3; }\n"))
	f.Add([]byte("edition = \"2023\";\nservice S { rpc M(stream E) returns (stream E); }\nmessage E {}\n"))
	f.Fuzz(func(t *testing.T, src []byte) {
		proto.Detect(src)
		set, err := proto.Compile("f.proto", src, []string{protoFixtures})
		if err != nil {
			return
		}
		renderSet(t, set)
	})
}

// FuzzDescriptorSet feeds raw bytes where the CLI accepts a binary
// FileDescriptorSet, the form a set produced by any other tool arrives in.
func FuzzDescriptorSet(f *testing.F) {
	for _, name := range []string{"haberdasher/v1/haberdasher.proto", "library/v1/library.proto"} {
		src, err := os.ReadFile(protoFixtures + "/" + name)
		if err != nil {
			f.Fatal(err)
		}
		set, err := proto.Compile(name, src, []string{protoFixtures})
		if err != nil {
			f.Fatal(err)
		}
		f.Add(set)
	}
	f.Add([]byte{0x0a, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		proto.Detect(data)
		renderSet(t, data)
	})
}

func renderSet(t *testing.T, set []byte) {
	t.Helper()
	for _, all := range []bool{false, true} {
		s, err := proto.Parse(set, all)
		if err != nil {
			return
		}
		data, _, err := Render(s, Options{Paper: docx.A4})
		if err != nil {
			t.Fatalf("Render (all=%v): %v", all, err)
		}
		wellFormed(t, data)
	}
}
