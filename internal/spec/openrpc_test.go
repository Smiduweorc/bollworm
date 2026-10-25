package spec

import (
	"os"
	"strings"
	"testing"
)

const rpcHead = "openrpc: 1.3.2\ninfo: {title: T, version: '1'}\n"

func features(t *testing.T) *Spec {
	t.Helper()
	src, err := os.ReadFile("../../testdata/openrpc-features.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, string(src))
}

func method(t *testing.T, s *Spec, name string) *Operation {
	t.Helper()
	for _, op := range ops(s) {
		if op.ID == name {
			return op
		}
	}
	t.Fatalf("no method %q", name)
	return nil
}

func TestOpenRPCVersionsAccepted(t *testing.T) {
	for _, v := range []string{"1.0.0", "1.0.0-rc1", "1.2.6", "1.3.2", "1"} {
		s, err := Parse([]byte("openrpc: "+v+"\ninfo: {title: t, version: '1'}\nmethods: []\n"), "", nil)
		if err != nil {
			t.Errorf("%s refused: %v", v, err)
			continue
		}
		if s.Format != OpenRPC || s.Version != v {
			t.Errorf("%s: format %d, version %q", v, s.Format, s.Version)
		}
	}
}

func TestOpenRPCRefusals(t *testing.T) {
	cases := []struct {
		src  string
		line int
		msg  string
	}{
		{"openrpc: 2.0.0\nmethods: []\n", 1, "OpenRPC 2.0.0 is not supported; bollworm reads 1.x"},
		{"openrpc: 10.0.0\nmethods: []\n", 1, "OpenRPC 10.0.0 is not supported"},
		{rpcHead, 0, "methods must be a list of method objects"},
		{rpcHead + "methods: {a: 1}\n", 3, "methods must be a list of method objects"},
		{rpcHead + "methods:\n  - summary: nameless\n", 4, "a method needs a name"},
		{rpcHead + "methods:\n  - {name: m, paramStructure: by-magic}\n", 4, `paramStructure must be "by-name", "by-position" or "either", not "by-magic"`},
		{rpcHead + "methods:\n  - name: m\n    params: [{name: p}]\n", 5, `content descriptor "p" needs a schema`},
		{rpcHead + "methods:\n  - name: m\n    result: {name: r}\n", 5, `content descriptor "r" needs a schema`},
		{rpcHead + "methods:\n  - name: m\n    params: [{$ref: '#/components/contentDescriptors/Gone'}]\n", 5, "Gone"},
	}
	for _, c := range cases {
		e := refusal(t, c.src)
		if e.Line != c.line || !strings.Contains(e.Msg, c.msg) {
			t.Errorf("%q: got line %d %q, want line %d containing %q", c.src, e.Line, e.Msg, c.line, c.msg)
		}
	}
}

func TestOpenRPCInfoAndServers(t *testing.T) {
	s := features(t)
	if s.Title != "Hive" || s.APIVersion != "2.1.0" || s.Contact.Email != "keeper@example.org" || s.License.Name != "MIT" {
		t.Errorf("info: %+v", s)
	}
	if len(s.Servers) != 2 || s.Servers[1].URL != "http://localhost:8545" || s.Servers[1].Description != "Local node" {
		t.Errorf("servers: %+v", s.Servers)
	}
}

func TestOpenRPCMethodsAreGroupedByFirstTag(t *testing.T) {
	s := features(t)
	var got []string
	for _, g := range s.Groups {
		var names []string
		for _, op := range g.Operations {
			names = append(names, op.ID)
		}
		got = append(got, g.Name+"="+strings.Join(names, ",")+" ("+g.Description+")")
	}
	want := []string{
		"frames=hive_count,hive_inspect (Calls about one frame of the hive.)",
		"watch=hive_subscribe (Calls that keep sending.)",
		"=hive_ping ()",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("groups:\n%s", strings.Join(got, "\n"))
	}
}

func TestOpenRPCLaterTagUseSuppliesTheDescription(t *testing.T) {
	s := parse(t, rpcHead+`methods:
  - {name: a, tags: [{name: x}]}
  - {name: b, tags: [{name: x, description: Second.}]}
  - {name: c, tags: [{name: x, description: Third.}]}
`)
	if len(s.Groups) != 1 || s.Groups[0].Description != "Second." {
		t.Errorf("groups: %+v", s.Groups)
	}
}

func TestOpenRPCParamsAndResult(t *testing.T) {
	s := features(t)
	count := method(t, s, "hive_count").JSONRPC
	if count.ParamStructure != "by-position" || len(count.Params) != 2 {
		t.Fatalf("hive_count: %+v", count)
	}
	frame := count.Params[0]
	if frame.Name != "frame" || !frame.Required || frame.Description != "The frame, counted from the entrance." || frame.Schema.Minimum != "1" || frame.In != "" {
		t.Errorf("frame: %+v", frame)
	}
	if caste := count.Params[1]; caste.Required || caste.Schema != model(t, s, "Caste") {
		t.Errorf("caste: %+v", caste)
	}
	if count.Result.Name != "count" || count.Result.Schema.Types[0] != "integer" {
		t.Errorf("result: %+v", count.Result)
	}

	inspect := method(t, s, "hive_inspect").JSONRPC
	if deep := inspect.Params[1]; !deep.Deprecated || deep.Description != "Look under the comb as well." {
		t.Errorf("a descriptor's summary stands in for its description: %+v", deep)
	}
	if inspect.Result.Schema != model(t, s, "Report") {
		t.Error("a result $ref is the model")
	}
	if inspect.Params[0].Schema != count.Params[0].Schema {
		t.Error("a content descriptor used twice is one schema")
	}
}

func TestOpenRPCDefaultsAndNotifications(t *testing.T) {
	s := features(t)
	ping := method(t, s, "hive_ping")
	if ping.JSONRPC.ParamStructure != "either" {
		t.Errorf("default paramStructure: %q", ping.JSONRPC.ParamStructure)
	}
	if ping.JSONRPC.Result != nil {
		t.Error("a method without a result is a notification")
	}
	if !ping.Deprecated || ping.Summary != "" || ping.HTTP != nil || ping.RPC != nil {
		t.Errorf("hive_ping: %+v", ping)
	}
	if sub := method(t, s, "hive_subscribe").JSONRPC; len(sub.Params) != 0 || sub.Result == nil {
		t.Errorf("hive_subscribe: %+v", sub)
	}
}

func TestOpenRPCErrorsAndExamples(t *testing.T) {
	count := method(t, features(t), "hive_count").JSONRPC
	if len(count.Errors) != 2 || count.Errors[0].Code != "4004" || count.Errors[0].Message != "There is no frame with that number" || count.Errors[1].Code != "-32602" {
		t.Errorf("errors: %+v", count.Errors)
	}
	if len(count.Examples) != 1 {
		t.Fatalf("examples: %+v", count.Examples)
	}
	x := count.Examples[0]
	if x.Name != "workers" || x.Summary != "Workers in frame 3" || len(x.Params) != 2 {
		t.Fatalf("example: %+v", x)
	}
	if x.Params[0].Name != "frame" || x.Params[0].Value.Value != "3" || x.Params[1].Name != "caste" || x.Params[1].Value.Value != "worker" {
		t.Errorf("example params: %+v %+v", x.Params[0], x.Params[1])
	}
	if x.Result.Value != "2180" {
		t.Errorf("example result: %+v", x.Result)
	}
}

func TestOpenRPCMethodRefIsFollowed(t *testing.T) {
	s := parse(t, rpcHead+`methods:
  - {$ref: '#/x-methods/m'}
x-methods:
  m: {name: shared, result: {name: r, schema: {type: string}}}
`)
	if op := method(t, s, "shared"); op.JSONRPC.Result.Schema.Types[0] != "string" {
		t.Errorf("method: %+v", op)
	}
}

func TestOpenRPCPublishedPetstore(t *testing.T) {
	src, err := os.ReadFile("../../testdata/openrpc-petstore.json")
	if err != nil {
		t.Fatal(err)
	}
	s := parse(t, string(src))
	var names []string
	for _, op := range ops(s) {
		names = append(names, op.ID)
	}
	if strings.Join(names, ",") != "get_pets,create_pet,get_pet_by_id,delete_pet_by_id" {
		t.Errorf("methods: %v", names)
	}
	if len(s.Models) != 2 || s.Models[0].Name != "Pet" || s.Models[1].Name != "NewPet" {
		t.Errorf("models: %+v", s.Models)
	}
}
