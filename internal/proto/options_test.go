package proto

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// fieldBehaviorProto declares google.api.field_behavior with the number and
// values googleapis gives it.
const fieldBehaviorProto = `syntax = "proto3";
package google.api;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions {
  repeated google.api.FieldBehavior field_behavior = 1052 [packed = false];
}
enum FieldBehavior {
  FIELD_BEHAVIOR_UNSPECIFIED = 0;
  OPTIONAL = 1;
  REQUIRED = 2;
  OUTPUT_ONLY = 3;
  INPUT_ONLY = 4;
  IMMUTABLE = 5;
}
`

// rulesProto is a custom option bollworm was not built with, shaped like
// buf.validate's.
const rulesProto = `syntax = "proto3";
package acme.rules;
import "google/protobuf/descriptor.proto";
message StringRules {
  uint64 min_len = 1;
  repeated string in = 2;
}
message FieldRules { StringRules string = 1; bool required = 2; }
extend google.protobuf.FieldOptions { FieldRules field = 51000; }
extend google.protobuf.MethodOptions { string audit = 51001; }
extend google.protobuf.MessageOptions { int32 ttl = 51002; }
`

func TestFieldBehaviorAndCustomOptions(t *testing.T) {
	s, err := source(t, map[string]string{
		"google/api/field_behavior.proto": fieldBehaviorProto,
		"acme/rules.proto":                rulesProto,
		"a.proto": `syntax = "proto3";
package a;
import "google/api/field_behavior.proto";
import "acme/rules.proto";
service S {
  rpc Get(A) returns (A) {
    option idempotency_level = NO_SIDE_EFFECTS;
    option (acme.rules.audit) = "reads";
  }
}
message A {
  option (acme.rules.ttl) = 30;
  string name = 1 [(google.api.field_behavior) = REQUIRED, (acme.rules.field) = {string: {min_len: 1, in: ["x", "y"]}}];
  string id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  string secret = 3 [(google.api.field_behavior) = INPUT_ONLY];
  string kind = 4 [(google.api.field_behavior) = IMMUTABLE];
  B b = 5 [(google.api.field_behavior) = OUTPUT_ONLY];
}
message B {}
`})
	if err != nil {
		t.Fatal(err)
	}
	a := model(t, s, "A")
	if !slices.Equal(a.Options, []string{"(acme.rules.ttl): 30"}) {
		t.Errorf("message options %q", a.Options)
	}
	name := field(t, a, "name")
	if !name.Required || !slices.Equal(name.Schema.Options, []string{`(acme.rules.field): {string: {min_len: 1, in: ["x", "y"]}}`}) {
		t.Errorf("name: required %v, options %q", name.Required, name.Schema.Options)
	}
	if !field(t, a, "id").Schema.ReadOnly || !field(t, a, "secret").Schema.WriteOnly || !field(t, a, "kind").Schema.Immutable {
		t.Error("field behaviors not read")
	}
	if b := field(t, a, "b").Schema; !b.ReadOnly || len(b.AllOf) != 1 || b.AllOf[0] != model(t, s, "B") {
		t.Errorf("a message field's behavior goes on a wrapper: %+v", b)
	}
	if op := rpc(t, s, "S", "Get"); !slices.Equal(op.Options, []string{"idempotency_level: NO_SIDE_EFFECTS", `(acme.rules.audit): "reads"`}) {
		t.Errorf("method options %q", op.Options)
	}
}

func TestIncludeImportsDescribesImportedServices(t *testing.T) {
	files := map[string]string{
		"dep.proto": `syntax = "proto3"; package dep; service Health { rpc Check(Ping) returns (Ping); } message Ping {}`,
		"a.proto":   `syntax = "proto3"; package a; import "dep.proto"; service S { rpc Get(dep.Ping) returns (dep.Ping); }`,
	}
	set := compileSources(t, files)
	for _, all := range []bool{false, true} {
		s, err := Parse(set, all)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, g := range s.Groups {
			names = append(names, g.Name)
		}
		want := []string{"S"}
		if all {
			want = []string{"Health", "S"}
		}
		if !slices.Equal(names, want) {
			t.Errorf("all=%v: services %q", all, names)
		}
	}
}

func TestEveryFieldBehaviorIsSpelledOut(t *testing.T) {
	s, err := source(t, map[string]string{
		"google/api/field_behavior.proto": strings.Replace(fieldBehaviorProto, "IMMUTABLE = 5;", "IMMUTABLE = 5;\n  UNORDERED_LIST = 6;\n  NON_EMPTY_DEFAULT = 7;\n  IDENTIFIER = 8;", 1),
		"a.proto": `syntax = "proto3";
package a;
import "google/api/field_behavior.proto";
message A {
  string name = 1 [(google.api.field_behavior) = IDENTIFIER];
  repeated string tags = 2 [(google.api.field_behavior) = UNORDERED_LIST, (google.api.field_behavior) = OPTIONAL];
  string region = 3 [(google.api.field_behavior) = NON_EMPTY_DEFAULT];
}
`})
	if err != nil {
		t.Fatal(err)
	}
	a := model(t, s, "A")
	for name, want := range map[string]string{"name": "Identifier:", "tags": "Unordered:", "region": "Never empty:"} {
		notes := field(t, a, name).Schema.Notes
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.HasPrefix(n, want) }) {
			t.Errorf("%s: notes %q", name, notes)
		}
	}
	if n := field(t, a, "tags").Schema.Notes; len(n) != 2 || !strings.HasPrefix(n[1], "Optional:") {
		t.Errorf("tags: %q", n)
	}
}

func TestUndefinedOptionsAreWarnedAbout(t *testing.T) {
	set := compileSources(t, map[string]string{
		"acme/rules.proto": rulesProto,
		"a.proto": `syntax = "proto3";
package a;
import "acme/rules.proto";
message A { option (acme.rules.ttl) = 30; }
`})
	// A set without the file that declares the option, as
	// buf build --exclude-imports makes.
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(set, &fds); err != nil {
		t.Fatal(err)
	}
	fds.File = slices.DeleteFunc(fds.File, func(f *descriptorpb.FileDescriptorProto) bool { return f.GetName() == "acme/rules.proto" })
	stripped, _ := proto.Marshal(&fds)
	s, err := Parse(stripped, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "a.A: option number 51002 is not defined") || !strings.HasPrefix(s.Warnings[0], "a.proto:4:1: ") {
		t.Errorf("warnings %q", s.Warnings)
	}
	if len(model(t, s, "A").Options) != 0 {
		t.Error("an unreadable option was printed")
	}
}

func TestEveryCompileErrorIsReported(t *testing.T) {
	_, err := source(t, map[string]string{"a.proto": `syntax = "proto3";
package a;
message A { Missing one = 1; }
message B { Gone two = 1; }
`})
	if err == nil || !strings.Contains(err.Error(), "Missing") || !strings.Contains(err.Error(), "Gone") {
		t.Errorf("got %v", err)
	}
}

func TestStrayControlByteIsNotASet(t *testing.T) {
	if set, _ := Detect([]byte("openapi: 3.1.0\x01\ninfo: {}\n")); set {
		t.Error("YAML with a control byte taken for a FileDescriptorSet")
	}
	if set, _ := Detect(compileSources(t, map[string]string{"a.proto": `syntax = "proto3"; package a;`})); !set {
		t.Error("a real set is not detected")
	}
}
