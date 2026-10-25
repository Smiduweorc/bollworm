package render

import (
	"os"
	"strings"
	"testing"
)

func hiveDocument(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../testdata/openrpc-features.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return document(t, string(src))
}

func TestOpenRPCIntroduction(t *testing.T) {
	text := visible(hiveDocument(t))
	if !strings.HasPrefix(text, "Hive\nAPI reference, version 2.1.0\n") {
		t.Errorf("title page: %q", text[:min(80, len(text))])
	}
	mustContain(t, text, "Servers\nEvery method can be called at any of the following URLs.\nURL | Description | \nhttps://hive.example.org/rpc | Production | \n")
	one := visible(document(t, "openrpc: 1.3.2\ninfo: {title: T, version: '1'}\nservers: [{url: 'https://a.example'}]\nmethods: []\n"))
	mustContain(t, one, "Every method is called at the following URL.\n")
	if strings.Contains(one+text, "Paths in this document") {
		t.Error("OpenRPC servers have no paths")
	}
}

func TestOpenRPCMethodSections(t *testing.T) {
	text := visible(hiveDocument(t))
	mustContain(t, section(text, "Count bees (hive_count)", "Inspect a frame"),
		"JSON-RPC  hive_count\nReturns how many bees are in a frame, or in the whole hive.\nParams\nParams are sent by position, as an array in the order below.\n",
		"# | Name | Type | Required | Description | \n1 | frame | integer | Yes | The frame, counted from the entrance.\nAt least 1 and at most 10. | \n2 | caste | Caste | No |  | \n",
		"Result: integer.\nAt least 0.\nThe number of bees.\n",
		"Errors\nCode | Message | \n4004 | There is no frame with that number | \n-32602 | Invalid params | \n",
		"Example: Workers in frame 3\nParams:\n[/  3,/  \"worker\"/]\nResult:\n2180\n",
	)
	inspect := section(text, "Inspect a frame", "watch")
	mustContain(t, inspect,
		"Params are sent by name, as an object.\n",
		"2 | deep | boolean | No | Look under the comb as well.\nDeprecated. Default: false. | \n",
		"Result: Report.\n",
		"Example: shallow\nParams:\n{/  \"frame\": 1/}\nResult:\n{/  \"frame\": 1,",
	)
	if strings.Contains(inspect, "Errors") {
		t.Error("a method without errors has no errors table")
	}
	mustContain(t, section(text, "Count bees (hive_subscribe)", "Other methods"),
		"This method takes no params.\nResult: string.\n")
	mustContain(t, section(text, "hive_ping", "Data models"),
		"Deprecated. This method is still served but may be removed; avoid it in new code.\n",
		"Params may be sent by name, as an object, or by position, as an array in the order below.\n",
		"Result: none. This method is a notification: the server sends no response.\n")
}

func TestOpenRPCChapters(t *testing.T) {
	text := visible(hiveDocument(t))
	mustContain(t, text, "\nframes\nCalls about one frame of the hive.\n", "\nwatch\nCalls that keep sending.\n", "\nOther methods\nhive_ping\n")
	only := visible(document(t, "openrpc: 1.3.2\ninfo: {title: T, version: '1'}\nmethods: [{name: m}]\n"))
	mustContain(t, only, "\nMethods\nm\nJSON-RPC  m\n")
}

func TestOpenRPCExampleWithoutAValueIsNull(t *testing.T) {
	text := visible(document(t, `openrpc: 1.3.2
info: {title: T, version: '1'}
methods:
  - name: m
    params: [{name: a, schema: {}}]
    examples: [{name: x, params: [{name: a}]}]
`))
	mustContain(t, text, "Example: x\nParams:\n{/  \"a\": null/}\n")
	if strings.Contains(section(text, "m", "Data models"), "Result:\n") {
		t.Error("an example without a result prints none")
	}
}

func TestOpenRPCLinksResolve(t *testing.T) {
	linksResolve(t, "openrpc-features", hiveDocument(t))
	src, err := os.ReadFile("../../testdata/openrpc-petstore.json")
	if err != nil {
		t.Fatal(err)
	}
	linksResolve(t, "openrpc-petstore", document(t, string(src)))
}
