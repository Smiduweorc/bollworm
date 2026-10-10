package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"
)

// unpack returns every part of a package by name, failing on any part that
// is not well-formed XML.
func unpack(t *testing.T, data []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		dec := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s is not well-formed: %v", f.Name, err)
			}
		}
		parts[f.Name] = string(b)
	}
	return parts
}

func body(t *testing.T, d *Doc) string {
	t.Helper()
	data, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return unpack(t, data)["word/document.xml"]
}

func TestEscDropsWhatXMLCannotCarry(t *testing.T) {
	cases := map[string]string{
		"a<b>&\"c'":            "a&lt;b&gt;&amp;&#34;c&#39;",
		"bell\x07nul\x00end":   "bellnulend",
		"tab\tnl\ncr\r":        "tab&#x9;nl&#xA;cr&#xD;",
		"bad\xffutf8":          "bad\uFFFDutf8",
		"\uFFFE\uFFFFok":       "ok",
		"caf\u00e9 \U0001F41B": "caf\u00e9 \U0001F41B",
		"":                     "",
	}
	for in, want := range cases {
		if got := esc(in); got != want {
			t.Errorf("esc(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestControlCharactersStillProduceAValidPackage(t *testing.T) {
	d := New("ti\x01tle", A4)
	d.Para(Para{Inlines: []Inline{{Text: "x\x02y", URL: "https://example.com/?a=1&b=\"2\""}}})
	data, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	parts := unpack(t, data)
	if !strings.Contains(parts["word/_rels/document.xml.rels"], `Target="https://example.com/?a=1&amp;b=&#34;2&#34;"`) {
		t.Errorf("link target not escaped:\n%s", parts["word/_rels/document.xml.rels"])
	}
}

func TestTableColumnsFillTheTextWidthExactly(t *testing.T) {
	for _, paper := range []Paper{A4, Letter} {
		d := New("t", paper)
		d.Table(Table{Widths: []int{1, 1, 1}, Header: []string{"a", "b", "c"}})
		re := regexp.MustCompile(`<w:gridCol w:w="(\d+)"/>`)
		sum := 0
		for _, m := range re.FindAllStringSubmatch(body(t, d), -1) {
			n := 0
			for _, c := range m[1] {
				n = n*10 + int(c-'0')
			}
			sum += n
		}
		// A4: 11906 - 2*1440 = 9026. Letter: 12240 - 2*1440 = 9360.
		want := map[Paper]int{A4: 9026, Letter: 9360}[paper]
		if sum != want {
			t.Errorf("%v: columns sum to %d, want %d", paper, sum, want)
		}
	}
}

func TestTableHeaderRepeatsAndRowsDoNotSplit(t *testing.T) {
	d := New("t", A4)
	d.Table(Table{Widths: []int{1, 2}, Header: []string{"H1", "H2"}, Rows: [][]Cell{{{{Inlines: []Inline{{Text: "a"}}}}, {}}}})
	b := body(t, d)
	if strings.Count(b, "<w:tblHeader/>") != 1 || strings.Count(b, "<w:cantSplit/>") != 2 {
		t.Errorf("header or row flags wrong:\n%s", b)
	}
	// The empty cell still holds a paragraph, or Word reports the file as
	// corrupt.
	if strings.Count(b, "<w:tc>") != 4 || strings.Count(b, "<w:tc><w:tcPr>") != 4 || strings.Contains(b, "</w:tcPr></w:tc>") {
		t.Errorf("a cell without a paragraph:\n%s", b)
	}
	if !strings.Contains(b, `</w:tbl><w:p><w:pPr><w:pStyle w:val="AfterTable"/>`) {
		t.Error("a table must be followed by a paragraph")
	}
}

func TestRunsSplitNewlinesIntoBreaks(t *testing.T) {
	d := New("t", A4)
	d.Para(Para{Inlines: []Inline{{Text: "one\ntwo"}, {Break: true}, {Text: ""}}})
	if b := body(t, d); !strings.Contains(b, `<w:t xml:space="preserve">one</w:t><w:br/><w:t xml:space="preserve">two</w:t></w:r><w:r><w:br/></w:r></w:p>`) {
		t.Errorf("got %s", b)
	}
}

func TestCodeParaKeepsIndentationAndExpandsTabs(t *testing.T) {
	p := CodePara("a\n\tb\n  c\n\n")
	var got []string
	for _, in := range p.Inlines {
		if in.Break {
			got = append(got, "|")
		} else {
			got = append(got, in.Text)
		}
	}
	if p.Style != "Code" || strings.Join(got, "") != "a|    b|  c" {
		t.Errorf("got %q", strings.Join(got, ""))
	}
}

func TestRunFormatting(t *testing.T) {
	d := New("t", A4)
	d.Para(Para{Inlines: []Inline{{Text: "x", Bold: true, Italic: true, Strike: true}, {Text: "c", Code: true}, {Text: "l", Code: true, URL: "https://example.com"}}})
	b := body(t, d)
	for _, want := range []string{
		`<w:rPr><w:b/><w:bCs/><w:i/><w:iCs/><w:strike/></w:rPr>`,
		`<w:rPr><w:rStyle w:val="CodeChar"/></w:rPr>`,
		`<w:rPr><w:rStyle w:val="Hyperlink"/><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/></w:rPr>`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %s in\n%s", want, b)
		}
	}
}

func TestLinksShareOneRelationshipPerURL(t *testing.T) {
	d := New("t", A4)
	d.Para(Para{Inlines: []Inline{{Text: "a", URL: "https://example.com/a"}, {Text: "b", URL: "https://example.com/b"}, {Text: "a again", URL: "https://example.com/a"}}})
	data, _ := d.Bytes()
	parts := unpack(t, data)
	rels := parts["word/_rels/document.xml.rels"]
	if strings.Count(rels, "TargetMode=\"External\"") != 2 || !strings.Contains(rels, `Id="rId5"`) || !strings.Contains(rels, `Id="rId6"`) {
		t.Errorf("rels:\n%s", rels)
	}
	if strings.Count(parts["word/document.xml"], `r:id="rId5"`) != 2 {
		t.Error("the repeated URL should reuse rId5")
	}
}

func TestInternalLinksPointAtBookmarks(t *testing.T) {
	d := New("t", A4)
	d.Heading(2, "Target", "Model_Target")
	d.Para(Para{Inlines: []Inline{{Text: "see", Anchor: "Model_Target"}}})
	b := body(t, d)
	if !strings.Contains(b, `<w:bookmarkStart w:id="1" w:name="Model_Target"/>`) || !strings.Contains(b, `<w:bookmarkEnd w:id="1"/>`) || !strings.Contains(b, `<w:hyperlink w:anchor="Model_Target"`) {
		t.Errorf("got %s", b)
	}
}

func TestBookmarkIDsAreUnique(t *testing.T) {
	d := New("t", A4)
	for range 3 {
		d.Heading(1, "h", "")
	}
	b := body(t, d)
	ids := regexp.MustCompile(`bookmarkStart w:id="(\d+)" w:name="([^"]+)"`).FindAllStringSubmatch(b, -1)
	seenID, seenName := map[string]bool{}, map[string]bool{}
	for _, m := range ids {
		if seenID[m[1]] || seenName[m[2]] {
			t.Errorf("duplicate bookmark %v", m)
		}
		seenID[m[1]], seenName[m[2]] = true, true
	}
	if len(ids) != 3 {
		t.Errorf("want 3 bookmarks, got %d", len(ids))
	}
}

func TestTOCListsHeadingsInBodyOrder(t *testing.T) {
	d := New("t", A4)
	d.TOC()
	d.Heading(1, "First", "")
	d.Heading(2, "First.a", "")
	d.Heading(3, "not listed", "")
	d.Heading(1, "Second", "")
	b := body(t, d)
	toc := regexp.MustCompile(`<w:pStyle w:val="TOC(\d)"/>.*?<w:hyperlink w:anchor="([^"]+)".*?<w:t xml:space="preserve">([^<]*)</w:t>`).FindAllStringSubmatch(b, -1)
	heads := regexp.MustCompile(`<w:pStyle w:val="Heading([12])"/></w:pPr><w:bookmarkStart w:id="\d+" w:name="([^"]+)"/><w:r><w:t xml:space="preserve">([^<]*)</w:t>`).FindAllStringSubmatch(b, -1)
	if len(toc) != 3 || len(heads) != 3 {
		t.Fatalf("toc %d entries, %d headings", len(toc), len(heads))
	}
	for i := range toc {
		if toc[i][1] != heads[i][1] || toc[i][2] != heads[i][2] || toc[i][3] != heads[i][3] {
			t.Errorf("entry %d: toc %v, heading %v", i, toc[i][1:], heads[i][1:])
		}
	}
	if strings.Index(b, "TOC1") > strings.Index(b, "Heading1") {
		t.Error("the contents must come before the body")
	}
	if strings.Count(b, `w:fldCharType="begin"`) != 1 || strings.Count(b, `w:fldCharType="end"`) != 1 {
		t.Error("one TOC field, opened once and closed once")
	}
}

func TestTOCWithNoHeadingsIsStillAField(t *testing.T) {
	d := New("t", A4)
	d.TOC()
	b := body(t, d)
	if !strings.Contains(b, `TOC \o "1-2" \h \z \u`) || strings.Count(b, `w:fldCharType="end"`) != 1 {
		t.Errorf("got %s", b)
	}
}

func TestListsGetTheirOwnNumbering(t *testing.T) {
	d := New("t", A4)
	a := d.NewList(false, 1)
	b := d.NewList(true, 4)
	d.Para(Para{List: &ListItem{ID: b, Level: 2}, Inlines: []Inline{{Text: "x"}}})
	data, _ := d.Bytes()
	parts := unpack(t, data)
	num := parts["word/numbering.xml"]
	if a != 1 || b != 2 ||
		!strings.Contains(num, `<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`) ||
		!strings.Contains(num, `<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="4"/></w:lvlOverride></w:num>`) {
		t.Errorf("numbering:\n%s", num)
	}
	if !strings.Contains(parts["word/document.xml"], `<w:numPr><w:ilvl w:val="2"/><w:numId w:val="2"/></w:numPr>`) {
		t.Error("list item not numbered")
	}
	// The abstract definitions must come before the instances.
	if strings.LastIndex(num, "<w:abstractNum ") > strings.Index(num, "<w:num ") {
		t.Error("abstractNum after num")
	}
}

func TestPackageIsCompleteAndDeterministic(t *testing.T) {
	build := func() []byte {
		d := New("Title & co", Letter)
		d.TOC()
		d.Heading(1, "H", "")
		d.Para(Para{Inlines: []Inline{{Text: "x", URL: "https://example.com"}}})
		data, err := d.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	a, b := build(), build()
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same document differ")
	}
	parts := unpack(t, a)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "docProps/core.xml", "docProps/app.xml", "word/document.xml", "word/_rels/document.xml.rels", "word/styles.xml", "word/settings.xml", "word/numbering.xml", "word/footer1.xml"} {
		if _, ok := parts[name]; !ok {
			t.Errorf("missing part %s", name)
		}
	}
	for name := range parts {
		if strings.HasSuffix(name, ".rels") || name == "[Content_Types].xml" {
			continue
		}
		if !strings.Contains(parts["[Content_Types].xml"], `PartName="/`+name+`"`) {
			t.Errorf("%s has no content type override", name)
		}
	}
	for _, target := range regexp.MustCompile(`Target="([^"]+)"`).FindAllStringSubmatch(parts["word/_rels/document.xml.rels"], -1) {
		if strings.HasPrefix(target[1], "http") {
			continue
		}
		if _, ok := parts["word/"+target[1]]; !ok {
			t.Errorf("relationship to missing part %s", target[1])
		}
	}
	if !strings.Contains(parts["docProps/core.xml"], "<dc:title>Title &amp; co</dc:title>") {
		t.Error("title not in core properties")
	}
	doc := parts["word/document.xml"]
	if !strings.Contains(doc, `<w:pgSz w:w="12240" w:h="15840"/>`) || !strings.Contains(doc, "<w:titlePg/>") {
		t.Error("page setup wrong")
	}
	if !strings.Contains(parts["word/footer1.xml"], `<w:tab w:val="right" w:pos="9360"/>`) || !strings.Contains(parts["word/footer1.xml"], " NUMPAGES ") {
		t.Error("footer wrong")
	}
	if !strings.Contains(parts["word/settings.xml"], `w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"`) {
		t.Error("without compatibility mode 15 Word opens the file in Compatibility Mode")
	}
	z, _ := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	for _, f := range z.File {
		if !f.Modified.Equal(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s stamped %v", f.Name, f.Modified)
		}
	}
}

func TestStylesUseWordsBuiltInNames(t *testing.T) {
	// Word recognises its built-in styles by these exact lowercase names;
	// "Heading 1" with a capital would be a new custom style that the
	// navigation pane and TOC field ignore.
	for _, want := range []string{
		`w:styleId="Heading1"><w:name w:val="heading 1"/>`,
		`w:styleId="Heading2"><w:name w:val="heading 2"/>`,
		`w:styleId="Heading3"><w:name w:val="heading 3"/>`,
		`w:styleId="TOC1"><w:name w:val="toc 1"/>`,
		`w:styleId="TOC2"><w:name w:val="toc 2"/>`,
		`w:styleId="TableGrid"><w:name w:val="Table Grid"/>`,
		`w:styleId="Footer"><w:name w:val="footer"/>`,
		`<w:outlineLvl w:val="0"/>`,
		`<w:outlineLvl w:val="1"/>`,
	} {
		if !strings.Contains(styles, want) {
			t.Errorf("styles missing %s", want)
		}
	}
	if strings.Count(styles, `w:default="1"`) != 4 {
		t.Error("one default each for paragraph, character, table and numbering styles")
	}
}

func TestEveryReferencedStyleIsDefined(t *testing.T) {
	d := New("t", A4)
	d.TOC()
	d.Heading(1, "a", "")
	d.Heading(2, "b", "")
	d.Heading(3, "c", "")
	d.Para(Para{Style: "Title"})
	d.Para(Para{Style: "Subtitle"})
	d.Para(Para{Style: "TOCHeading"})
	d.Para(Para{Style: "ListParagraph"})
	d.Para(Para{Style: "Quote"})
	d.Para(Para{Style: "Endpoint"})
	d.Para(Para{Style: "Label"})
	d.Code("x")
	d.Para(Para{Inlines: []Inline{{Text: "c", Code: true}, {Text: "l", URL: "https://example.com"}}})
	d.Table(Table{Widths: []int{1}, Header: []string{"h"}})
	data, _ := d.Bytes()
	parts := unpack(t, data)
	used := regexp.MustCompile(`w:(?:pStyle|rStyle|tblStyle) w:val="([^"]+)"`).FindAllStringSubmatch(parts["word/document.xml"]+parts["word/footer1.xml"], -1)
	for _, u := range used {
		if !strings.Contains(styles, `w:styleId="`+u[1]+`"`) {
			t.Errorf("style %s is used but not defined", u[1])
		}
	}
}
