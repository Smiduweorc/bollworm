package render

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"gopkg.in/yaml.v3"
)

func convert(t *testing.T, src string) []block {
	t.Helper()
	return markdown(docx.New("t", docx.A4), nil, src)
}

func one(t *testing.T, src string) docx.Para {
	t.Helper()
	bs := convert(t, src)
	if len(bs) != 1 || bs[0].para == nil {
		t.Fatalf("%q: want one paragraph, got %d blocks", src, len(bs))
	}
	return *bs[0].para
}

func TestEmptyDescriptionIsNothing(t *testing.T) {
	for _, src := range []string{"", "  \n\t\n"} {
		if bs := convert(t, src); bs != nil {
			t.Errorf("%q gave %d blocks", src, len(bs))
		}
	}
}

func TestInlineFormatting(t *testing.T) {
	p := one(t, "plain *it* **bold** ***both*** ~~gone~~ `co*de*`")
	want := []docx.Inline{
		{Text: "plain "},
		{Text: "it", Italic: true},
		{Text: " "},
		{Text: "bold", Bold: true},
		{Text: " "},
		{Text: "both", Bold: true, Italic: true},
		{Text: " "},
		{Text: "gone", Strike: true},
		{Text: " "},
		{Text: "co*de*", Code: true},
	}
	if len(p.Inlines) != len(want) {
		t.Fatalf("got %+v", p.Inlines)
	}
	for i := range want {
		if p.Inlines[i] != want[i] {
			t.Errorf("inline %d: got %+v, want %+v", i, p.Inlines[i], want[i])
		}
	}
}

func TestEscapesAndEntitiesBecomeCharacters(t *testing.T) {
	if got := plain(one(t, `a &amp; b &lt;c&gt; \*not em\* &#169; &#x1F41B;`).Inlines); got != "a & b <c> *not em* \u00a9 \U0001F41B" {
		t.Errorf("got %q", got)
	}
}

func TestLineBreaks(t *testing.T) {
	p := one(t, "soft\nwrap  \nhard\\\nalso<br>html<BR />too")
	if got := plain(p.Inlines); got != "soft wrap hard also html too" {
		t.Errorf("got %q", got)
	}
	breaks := 0
	for _, in := range p.Inlines {
		if in.Break {
			breaks++
		}
	}
	if breaks != 4 {
		t.Errorf("want 4 breaks, got %d", breaks)
	}
}

func TestLinks(t *testing.T) {
	cases := []struct {
		src       string
		text, url string
	}{
		{"[site](https://example.com)", "site", "https://example.com"},
		{"[**bold** site](http://example.com)", "bold site", "http://example.com"},
		{"<https://example.com/x>", "https://example.com/x", "https://example.com/x"},
		{"<desk@example.com>", "desk@example.com", "mailto:desk@example.com"},
		{"https://example.com/auto", "https://example.com/auto", "https://example.com/auto"},
		{"[mail](MAILTO:a@example.com)", "mail", "MAILTO:a@example.com"},
		{"![logo](https://example.com/l.png)", "logo", "https://example.com/l.png"},
	}
	for _, c := range cases {
		p := one(t, c.src)
		if plain(p.Inlines) != c.text {
			t.Errorf("%q: text %q", c.src, plain(p.Inlines))
		}
		for _, in := range p.Inlines {
			if in.URL != c.url {
				t.Errorf("%q: inline %+v, want URL %q", c.src, in, c.url)
			}
		}
	}
}

func TestLinksAReaderCannotFollowArePrintedInstead(t *testing.T) {
	cases := map[string]string{
		"see [here](/rpc/olddoc.html)":           "see here (/rpc/olddoc.html)",
		"[x](javascript:alert(1))":               "x (javascript:alert(1))",
		"[ftp://example.com](ftp://example.com)": "ftp://example.com",
		"[nothing]()":                            "nothing",
	}
	for src, want := range cases {
		p := one(t, src)
		if got := plain(p.Inlines); got != want {
			t.Errorf("%q: got %q, want %q", src, got, want)
		}
		for _, in := range p.Inlines {
			if in.URL != "" {
				t.Errorf("%q: %+v must not be a link", src, in)
			}
		}
	}
}

func TestHeadingsBecomeBoldParagraphs(t *testing.T) {
	p := one(t, "## Rate *limits*")
	if !p.KeepNext || plain(p.Inlines) != "Rate limits" || !p.Inlines[0].Bold || !p.Inlines[1].Bold || !p.Inlines[1].Italic {
		t.Errorf("got %+v", p)
	}
}

func TestCodeBlocks(t *testing.T) {
	for _, src := range []string{"```json\n{\n\t\"a\": 1\n}\n```", "    {\n    \t\"a\": 1\n    }"} {
		p := one(t, src)
		// plain shows each line break as a space; the tab is four spaces.
		if p.Style != "Code" || plain(p.Inlines) != "{     \"a\": 1 }" {
			t.Errorf("%q: got %q in %q", src, plain(p.Inlines), p.Style)
		}
	}
}

func TestLists(t *testing.T) {
	doc := docx.New("t", docx.A4)
	bs := markdown(doc, nil, "- a\n- b\n  1. b1\n  2. b2\n\n     b2 more\n- c\n\n3. three\n4. four\n")
	type row struct {
		text  string
		style string
		id    int
		level int
	}
	var got []row
	for _, b := range bs {
		r := row{text: plain(b.para.Inlines), style: b.para.Style}
		if b.para.List != nil {
			r.id, r.level = b.para.List.ID, b.para.List.Level
		}
		got = append(got, r)
	}
	want := []row{
		{"a", "ListParagraph", 1, 0},
		{"b", "ListParagraph", 1, 0},
		{"b1", "ListParagraph", 2, 1},
		{"b2", "ListParagraph", 2, 1},
		{"b2 more", "ListParagraph", 0, 0},
		{"c", "ListParagraph", 1, 0},
		{"three", "ListParagraph", 3, 0},
		{"four", "ListParagraph", 3, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	data, _ := doc.Bytes()
	num := wellFormedPart(t, data, "word/numbering.xml")
	for _, frag := range []string{
		`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`,
		`<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/>`,
		`<w:num w:numId="3"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="3"/>`,
	} {
		if !strings.Contains(num, frag) {
			t.Errorf("numbering missing %s", frag)
		}
	}
}

func TestDeepListsStopAtWordsLastLevel(t *testing.T) {
	src := ""
	for i := range 11 {
		src += strings.Repeat("  ", i) + "- x\n"
	}
	max := 0
	for _, b := range convert(t, src) {
		if b.para.List != nil {
			max = b.para.List.Level
		}
	}
	if max != 8 {
		t.Errorf("deepest level %d, want 8", max)
	}
}

func TestTaskListsBlockquotesAndBreaks(t *testing.T) {
	bs := convert(t, "- [x] done\n- [ ] todo\n\n> quoted\n\n---\n\n<div>raw</div>\n")
	var got []string
	for _, b := range bs {
		got = append(got, b.para.Style+":"+plain(b.para.Inlines))
	}
	want := "ListParagraph:[x] done|ListParagraph:[ ] todo|Quote:quoted|:<div>raw</div>"
	if strings.Join(got, "|") != want {
		t.Errorf("got %q", strings.Join(got, "|"))
	}
}

func TestTables(t *testing.T) {
	bs := convert(t, "| A | B |\n| --- | --- |\n| 1 | **2** |\n| 3 |\n")
	if len(bs) != 1 || bs[0].table == nil {
		t.Fatalf("got %+v", bs)
	}
	tb := bs[0].table
	if strings.Join(tb.Header, ",") != "A,B" || len(tb.Widths) != 2 || len(tb.Rows) != 2 {
		t.Fatalf("table %+v", tb)
	}
	if plain(tb.Rows[0][1][0].Inlines) != "2" || !tb.Rows[0][1][0].Inlines[0].Bold {
		t.Error("cell formatting lost")
	}
	if len(tb.Rows[1]) != 2 {
		t.Error("a short row is padded to the header's width")
	}
}

func TestTablesInCellsAreFlattened(t *testing.T) {
	r := &renderer{doc: docx.New("t", docx.A4)}
	c := r.cell("Codes:\n\n| Code | Meaning |\n| --- | --- |\n| 1 | one |\n", []string{"Note."})
	var got []string
	for _, p := range c {
		got = append(got, plain(p.Inlines))
	}
	if strings.Join(got, "/") != "Codes:/Code | Meaning/1 | one/Note." {
		t.Errorf("got %q", strings.Join(got, "/"))
	}
}

func TestExampleJSONKeepsKeyOrder(t *testing.T) {
	got := exampleJSON(node(t, "{zeta: 1, alpha: [true, null, {}], mid: {inner: []}, s: \"q\\\"<&>\"}"))
	want := `{
  "zeta": 1,
  "alpha": [
    true,
    null,
    {}
  ],
  "mid": {
    "inner": []
  },
  "s": "q\"<&>"
}`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestScalarsBecomeJSON(t *testing.T) {
	cases := map[string]string{
		"1":                    "1",
		"-3":                   "-3",
		"0x1F":                 "31",
		"0o17":                 "15",
		"1_000":                "1000",
		"1.5":                  "1.5",
		"1e3":                  "1e3",
		".5":                   "0.5",
		".inf":                 `".inf"`,
		"-.Inf":                `"-.Inf"`,
		".nan":                 `".nan"`,
		"true":                 "true",
		"True":                 "true",
		"FALSE":                "false",
		"null":                 "null",
		"~":                    "null",
		"2024-01-02":           `"2024-01-02"`,
		"plain":                `"plain"`,
		`"0x1F"`:               `"0x1F"`,
		"99999999999999999999": "99999999999999999999",
	}
	for in, want := range cases {
		if got := exampleJSON(node(t, in)); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestInlineExampleLength(t *testing.T) {
	short, ok := inlineExample(node(t, "{a: 1, b: [x]}"))
	if !ok || short != `{"a":1,"b":["x"]}` {
		t.Errorf("got %q %v", short, ok)
	}
	if _, ok := inlineExample(node(t, `"`+strings.Repeat("x", 58)+`"`)); !ok {
		t.Error("a 60-character example fits")
	}
	if _, ok := inlineExample(node(t, `"`+strings.Repeat("x", 59)+`"`)); ok {
		t.Error("a 61-character example does not")
	}
}

func TestAliasBombIsCutShort(t *testing.T) {
	src := "a: &a [x, x, x, x, x, x, x, x, x, x]\n"
	prev := "a"
	for _, l := range "bcdefghij" {
		name := string(l)
		src += name + ": &" + name + " [" + strings.Repeat("*"+prev+", ", 9) + "*" + prev + "]\n"
		prev = name
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	got := exampleJSON(root.Content[len(root.Content)-1])
	if !strings.HasSuffix(got, "\n"+truncated) || len(got) > exampleBudget+len(truncated)+1 {
		t.Errorf("example of %d bytes, ends %q", len(got), got[max(0, len(got)-40):])
	}
}

func node(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func wellFormedPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	wellFormed(t, data)
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == name {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatalf("no part %s", name)
	return ""
}
