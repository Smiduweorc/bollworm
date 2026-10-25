// Package docx writes WordprocessingML packages. It knows nothing about
// OpenAPI; it exists so the renderer can say "a heading, a table, a code
// block" and get back a file Word opens without a repair prompt.
//
// Word validates element order against the ECMA-376 schema and refuses or
// "repairs" a document whose children are out of sequence, so every
// properties block below is written in schema order. Styles use Word's own
// built-in names ("heading 1", "toc 1", "Table Grid"), which is what makes
// Word treat them as its own: the navigation pane, the TOC field and the
// style gallery all key on those names.
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Paper is a page size in twentieths of a point.
type Paper struct {
	Width, Height int
}

var (
	A4     = Paper{11906, 16838}
	Letter = Paper{12240, 15840}
)

// margin is one inch on every side, Word's default.
const margin = 1440

// Inline is a run of text with its formatting. URL makes it an external
// link; Anchor makes it a link to a bookmark in the document.
type Inline struct {
	Text   string
	Bold   bool
	Italic bool
	Strike bool
	Code   bool
	URL    string
	Anchor string
	// Break is a line break inside the paragraph; Text is ignored.
	Break bool
}

// Para is one paragraph.
type Para struct {
	Style    string
	Inlines  []Inline
	KeepNext bool
	// List, when set, puts the paragraph in a bulleted or numbered list.
	List *ListItem
	// Bookmark names a bookmark wrapped around the paragraph's text.
	Bookmark string
}

type ListItem struct {
	ID    int
	Level int
}

// Cell is a table cell holding one or more paragraphs.
type Cell []Para

// Table is a grid with one header row that repeats on every page.
type Table struct {
	// Widths are relative column weights; the table always fills the text
	// width.
	Widths []int
	Header []string
	Rows   [][]Cell
}

// Doc accumulates the body of a document.
type Doc struct {
	Title string
	Paper Paper
	// TOCDepth is the deepest heading level the contents list, 1 to 3.
	TOCDepth int
	body     bytes.Buffer
	links    []string
	linkID   map[string]string
	lists    []listDef
	marks    int
	toc      []tocEntry
	tocAt    int
	hasTOC   bool
}

type listDef struct {
	ordered bool
	start   int
}

func New(title string, paper Paper) *Doc {
	return &Doc{Title: title, Paper: paper, TOCDepth: 2, linkID: map[string]string{}}
}

// TextWidth is the width between the margins, in twentieths of a point.
func (d *Doc) TextWidth() int {
	return d.Paper.Width - 2*margin
}

// NewList starts a list. Each numbered list gets its own instance so its
// numbering starts again at start rather than continuing the last list.
func (d *Doc) NewList(ordered bool, start int) int {
	d.lists = append(d.lists, listDef{ordered, start})
	return len(d.lists)
}

func (d *Doc) Para(p Para) {
	writePara(&d.body, d, p)
}

// Code writes preformatted text as one shaded block.
func (d *Doc) Code(text string) {
	d.Para(CodePara(text))
}

// CodePara is the paragraph Code writes, for use inside a table cell.
func CodePara(text string) Para {
	text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", "    ")
	var ins []Inline
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			ins = append(ins, Inline{Break: true})
		}
		ins = append(ins, Inline{Text: line})
	}
	return Para{Style: "Code", Inlines: ins}
}

func (d *Doc) Table(t Table) {
	b := &d.body
	total := 0
	for _, w := range t.Widths {
		total += w
	}
	width := d.TextWidth()
	cols := make([]int, len(t.Widths))
	used := 0
	for i, w := range t.Widths {
		cols[i] = width * w / total
		used += cols[i]
	}
	cols[len(cols)-1] += width - used

	b.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/><w:tblLayout w:type="fixed"/><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="1" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for _, c := range cols {
		fmt.Fprintf(b, `<w:gridCol w:w="%d"/>`, c)
	}
	b.WriteString(`</w:tblGrid>`)

	b.WriteString(`<w:tr><w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>`)
	for i, h := range t.Header {
		fmt.Fprintf(b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/><w:shd w:val="clear" w:color="auto" w:fill="DCE6F2"/></w:tcPr>`, cols[i])
		writePara(b, d, Para{Style: "TableText", KeepNext: true, Inlines: []Inline{{Text: h, Bold: true}}})
		b.WriteString(`</w:tc>`)
	}
	b.WriteString(`</w:tr>`)

	for _, row := range t.Rows {
		b.WriteString(`<w:tr><w:trPr><w:cantSplit/></w:trPr>`)
		for i, cell := range row {
			fmt.Fprintf(b, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>`, cols[i])
			if len(cell) == 0 {
				// A cell must hold at least one paragraph.
				cell = Cell{{}}
			}
			for _, p := range cell {
				if p.Style == "" {
					p.Style = "TableText"
				}
				writePara(b, d, p)
			}
			b.WriteString(`</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	// Two tables in a row merge into one in Word; a paragraph between them
	// keeps them apart and gives the table room below.
	writePara(b, d, Para{Style: "AfterTable"})
}

// Heading writes a heading. Levels down to TOCDepth are listed in the
// table of contents. anchor names the bookmark other parts of the document
// link to; pass "" when nothing links here and one is made up for the
// contents.
func (d *Doc) Heading(level int, text, anchor string) {
	if anchor == "" {
		anchor = d.Bookmark()
	}
	if level <= d.TOCDepth {
		d.toc = append(d.toc, tocEntry{level, text, anchor})
	}
	d.Para(Para{Style: "Heading" + strconv.Itoa(level), Inlines: []Inline{{Text: text}}, Bookmark: anchor})
}

type tocEntry struct {
	level  int
	text   string
	anchor string
}

// TOC marks where the table of contents goes. It is written when the
// document is assembled, once every heading is known.
func (d *Doc) TOC() {
	d.tocAt = d.body.Len()
	d.hasTOC = true
}

// tocXML is a TOC field over heading levels 1 to TOCDepth. Word fills in page
// numbers when it updates fields on open; until then the entries show
// without them, so a viewer that never updates fields still gets a working
// list of links.
func (d *Doc) tocXML() string {
	var b bytes.Buffer
	begin := `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-` + strconv.Itoa(d.TOCDepth) + `" \h \z \u </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`
	end := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
	if len(d.toc) == 0 {
		b.WriteString(`<w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr>` + begin + end + `</w:p>`)
		return b.String()
	}
	for i, e := range d.toc {
		fmt.Fprintf(&b, `<w:p><w:pPr><w:pStyle w:val="TOC%d"/></w:pPr>`, e.level)
		if i == 0 {
			b.WriteString(begin)
		}
		fmt.Fprintf(&b, `<w:hyperlink w:anchor="%s" w:history="1">`, esc(e.anchor))
		writeRun(&b, Inline{Text: e.text}, "")
		b.WriteString(`</w:hyperlink>`)
		if i == len(d.toc)-1 {
			b.WriteString(end)
		}
		b.WriteString(`</w:p>`)
	}
	return b.String()
}

// Bookmark returns a fresh bookmark name. Names starting with an underscore
// are hidden from Word's bookmark dialog, as Word's own _Toc marks are.
func (d *Doc) Bookmark() string {
	d.marks++
	return fmt.Sprintf("_Toc%08d", d.marks)
}

func writePara(b *bytes.Buffer, d *Doc, p Para) {
	b.WriteString(`<w:p>`)
	if p.Style != "" || p.KeepNext || p.List != nil {
		b.WriteString(`<w:pPr>`)
		if p.Style != "" {
			fmt.Fprintf(b, `<w:pStyle w:val="%s"/>`, p.Style)
		}
		if p.KeepNext {
			b.WriteString(`<w:keepNext/>`)
		}
		if p.List != nil {
			fmt.Fprintf(b, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, p.List.Level, p.List.ID)
		}
		b.WriteString(`</w:pPr>`)
	}
	closing := `</w:p>`
	if p.Bookmark != "" {
		d.marks++
		fmt.Fprintf(b, `<w:bookmarkStart w:id="%d" w:name="%s"/>`, d.marks, esc(p.Bookmark))
		closing = fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/></w:p>`, d.marks)
	}
	for _, in := range p.Inlines {
		switch {
		case in.Anchor != "":
			fmt.Fprintf(b, `<w:hyperlink w:anchor="%s" w:history="1">`, esc(in.Anchor))
			writeRun(b, in, "Hyperlink")
			b.WriteString(`</w:hyperlink>`)
		case in.URL != "":
			fmt.Fprintf(b, `<w:hyperlink r:id="%s" w:history="1">`, d.rel(in.URL))
			writeRun(b, in, "Hyperlink")
			b.WriteString(`</w:hyperlink>`)
		default:
			writeRun(b, in, "")
		}
	}
	b.WriteString(closing)
}

func writeRun(b *bytes.Buffer, in Inline, charStyle string) {
	if in.Break {
		b.WriteString(`<w:r><w:br/></w:r>`)
		return
	}
	if in.Text == "" {
		return
	}
	b.WriteString(`<w:r>`)
	if in.Code && charStyle == "" {
		charStyle = "CodeChar"
	}
	if charStyle != "" || in.Bold || in.Italic || in.Strike || in.Code {
		b.WriteString(`<w:rPr>`)
		if charStyle != "" {
			fmt.Fprintf(b, `<w:rStyle w:val="%s"/>`, charStyle)
		}
		if in.Code && charStyle != "CodeChar" {
			b.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
		}
		if in.Bold {
			b.WriteString(`<w:b/><w:bCs/>`)
		}
		if in.Italic {
			b.WriteString(`<w:i/><w:iCs/>`)
		}
		if in.Strike {
			b.WriteString(`<w:strike/>`)
		}
		b.WriteString(`</w:rPr>`)
	}
	lines := strings.Split(in.Text, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteString(`<w:br/>`)
		}
		if line != "" {
			b.WriteString(`<w:t xml:space="preserve">`)
			b.WriteString(esc(line))
			b.WriteString(`</w:t>`)
		}
	}
	b.WriteString(`</w:r>`)
}

func (d *Doc) rel(url string) string {
	if id, ok := d.linkID[url]; ok {
		return id
	}
	d.links = append(d.links, url)
	// rId1 to rId4 belong to the fixed parts listed in documentRels.
	id := "rId" + strconv.Itoa(len(d.links)+4)
	d.linkID[url] = id
	return id
}

// esc escapes text for XML and drops what XML 1.0 cannot carry at all:
// most C0 control characters, lone surrogates, U+FFFE and U+FFFF. A spec
// string with a stray control byte would otherwise produce a file Word
// refuses to open.
func esc(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	clean := strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r >= 0xD800 && r <= 0xDFFF, r == 0xFFFE, r == 0xFFFF:
			return -1
		}
		return r
	}, s)
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(clean))
	return b.String()
}

// Bytes assembles the package. The zip entries carry a fixed timestamp so
// the same input always produces the same bytes.
func (d *Doc) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	stamp := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, part := range d.parts() {
		w, err := z.CreateHeader(&zip.FileHeader{Name: part.name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(part.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type part struct {
	name, body string
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const nsW = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func (d *Doc) parts() []part {
	return []part{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"docProps/core.xml", d.core()},
		{"docProps/app.xml", appProps},
		{"word/document.xml", d.document()},
		{"word/_rels/document.xml.rels", d.documentRels()},
		{"word/styles.xml", styles},
		{"word/settings.xml", settings},
		{"word/numbering.xml", d.numbering()},
		{"word/footer1.xml", d.footer()},
		{"word/fontTable.xml", fontTable},
	}
}

const contentTypes = xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>` +
	`<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
	`</Types>`

const packageRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appProps = xmlHeader + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>bollworm</Application></Properties>`

func (d *Doc) core() string {
	return xmlHeader + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title>` + esc(d.Title) + `</dc:title></cp:coreProperties>`
}

func (d *Doc) documentRels() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	b.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	b.WriteString(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>`)
	b.WriteString(`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
	b.WriteString(`<Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>`)
	b.WriteString(`<Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/>`)
	for _, u := range d.links {
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, d.linkID[u], escAttr(u))
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func escAttr(s string) string {
	return strings.ReplaceAll(esc(s), `"`, "&quot;")
}

func (d *Doc) document() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:document ` + nsW + `><w:body>`)
	body := d.body.Bytes()
	if d.hasTOC {
		b.Write(body[:d.tocAt])
		b.WriteString(d.tocXML())
		b.Write(body[d.tocAt:])
	} else {
		b.Write(body)
	}
	// titlePg with no first-page footer leaves the title page bare.
	fmt.Fprintf(&b, `<w:sectPr><w:footerReference w:type="default" r:id="rId4"/><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="708" w:footer="708" w:gutter="0"/><w:cols w:space="708"/><w:titlePg/><w:docGrid w:linePitch="360"/></w:sectPr>`,
		d.Paper.Width, d.Paper.Height, margin, margin, margin, margin)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func (d *Doc) footer() string {
	var b bytes.Buffer
	b.WriteString(xmlHeader + `<w:ftr ` + nsW + `>`)
	fmt.Fprintf(&b, `<w:p><w:pPr><w:pStyle w:val="Footer"/><w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs></w:pPr>`, d.TextWidth())
	writeRun(&b, Inline{Text: d.Title}, "")
	b.WriteString(`<w:r><w:tab/><w:t xml:space="preserve">Page </w:t></w:r>`)
	b.WriteString(field("PAGE", "1"))
	b.WriteString(`<w:r><w:t xml:space="preserve"> of </w:t></w:r>`)
	b.WriteString(field("NUMPAGES", "1"))
	b.WriteString(`</w:p></w:ftr>`)
	return b.String()
}

func field(instr, cached string) string {
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + instr + ` </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>` + cached + `</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

func (d *Doc) numbering() string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<w:numbering ` + nsW + `>`)
	bullets := []string{"\u2022", "o", "\u25AA"}
	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for lvl := range 9 {
		sym := bullets[lvl%3]
		font := `<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New" w:hint="default"/>`
		if sym != "o" {
			font = `<w:rFonts w:ascii="Aptos" w:hAnsi="Aptos" w:hint="default"/>`
		}
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr><w:rPr>%s</w:rPr></w:lvl>`,
			lvl, sym, 720*(lvl+1), font)
	}
	b.WriteString(`</w:abstractNum>`)
	formats := []string{"decimal", "lowerLetter", "lowerRoman"}
	b.WriteString(`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for lvl := range 9 {
		fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%%%d."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			lvl, formats[lvl%3], lvl+1, 720*(lvl+1))
	}
	b.WriteString(`</w:abstractNum>`)
	for i, l := range d.lists {
		if l.ordered {
			fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="%d"/></w:lvlOverride></w:num>`, i+1, l.start)
		} else {
			fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="0"/></w:num>`, i+1)
		}
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}
