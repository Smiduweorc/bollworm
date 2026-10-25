package render

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// OpenAPI says description fields are CommonMark. Specs written for GitHub
// renderers also use its tables and strikethrough, so GFM is on.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// block is a paragraph or a table: the two things a description turns into.
type block struct {
	para  *docx.Para
	table *docx.Table
}

type mdConv struct {
	doc   *docx.Doc
	links *linker
	src   []byte
	out   []block
	// depth is how many lists enclose the node being converted.
	depth int
}

// markdown converts a description into blocks. Lists are registered with
// doc so their numbering is in place when the blocks are written.
func markdown(doc *docx.Doc, links *linker, src string) []block {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	c := &mdConv{doc: doc, links: links, src: []byte(src)}
	root := md.Parser().Parse(text.NewReader(c.src))
	c.blocks(root, "")
	return c.out
}

func (c *mdConv) para(p docx.Para) {
	c.out = append(c.out, block{para: &p})
}

func (c *mdConv) blocks(n ast.Node, style string) {
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		c.block(ch, style, nil)
	}
}

// block converts one block node into paragraphs of the given style (empty
// for Normal). item, when it points at a list bullet, puts the first
// paragraph on that bullet and is cleared, so later paragraphs of the same
// item sit under the text instead of on bullets of their own.
func (c *mdConv) block(n ast.Node, style string, item **docx.ListItem) {
	take := func() *docx.ListItem {
		if item == nil {
			return nil
		}
		it := *item
		*item = nil
		return it
	}
	switch b := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		c.para(docx.Para{Style: style, Inlines: c.inlines(b, docx.Inline{}), List: take()})
	case *ast.Heading:
		c.para(docx.Para{Style: style, KeepNext: true, Inlines: c.inlines(b, docx.Inline{Bold: true}), List: take()})
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		c.para(docx.CodePara(c.lines(b)))
	case *ast.HTMLBlock:
		c.para(docx.Para{Style: style, Inlines: []docx.Inline{{Text: strings.TrimRight(c.lines(b), "\n")}}, List: take()})
	case *ast.Blockquote:
		c.blocks(b, "Quote")
	case *ast.List:
		c.list(b)
	case *extast.Table:
		c.table(b)
	case *ast.ThematicBreak:
	default:
		for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
			c.block(ch, style, item)
		}
	}
}

func (c *mdConv) list(l *ast.List) {
	start := l.Start
	if start == 0 {
		start = 1
	}
	id := c.doc.NewList(l.IsOrdered(), start)
	level := min(c.depth, 8)
	c.depth++
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		item := &docx.ListItem{ID: id, Level: level}
		for ch := it.FirstChild(); ch != nil; ch = ch.NextSibling() {
			c.block(ch, "ListParagraph", &item)
		}
	}
	c.depth--
}

func (c *mdConv) table(t *extast.Table) {
	var tbl docx.Table
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []docx.Cell
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, docx.Cell{{Inlines: c.inlines(cell, docx.Inline{})}})
		}
		if _, ok := row.(*extast.TableHeader); ok {
			for _, cell := range cells {
				tbl.Header = append(tbl.Header, plain(cell[0].Inlines))
			}
			continue
		}
		tbl.Rows = append(tbl.Rows, cells)
	}
	for range tbl.Header {
		tbl.Widths = append(tbl.Widths, 1)
	}
	// GFM pads short rows; a long row is cut to the header's width.
	for i, r := range tbl.Rows {
		for len(r) < len(tbl.Header) {
			r = append(r, docx.Cell{})
		}
		tbl.Rows[i] = r[:len(tbl.Header)]
	}
	c.out = append(c.out, block{table: &tbl})
}

func (c *mdConv) lines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(c.src))
	}
	return b.String()
}

var brTag = regexp.MustCompile(`(?i)^<br\s*/?>$`)

// inlines converts n's inline children, each inheriting the formatting in f.
func (c *mdConv) inlines(n ast.Node, f docx.Inline) []docx.Inline {
	var out []docx.Inline
	add := func(s string) {
		in := f
		in.Text = s
		out = append(out, in)
	}
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch t := ch.(type) {
		case *ast.Text:
			add(unescape(t.Segment.Value(c.src)))
			switch {
			case t.HardLineBreak():
				out = append(out, docx.Inline{Break: true})
			case t.SoftLineBreak():
				add(" ")
			}
		case *ast.String:
			add(string(t.Value))
		case *ast.CodeSpan:
			var b strings.Builder
			for s := t.FirstChild(); s != nil; s = s.NextSibling() {
				if tx, ok := s.(*ast.Text); ok {
					b.Write(tx.Segment.Value(c.src))
				}
			}
			in := f
			in.Text, in.Code = b.String(), true
			out = append(out, in)
		case *ast.Emphasis:
			g := f
			if t.Level >= 2 {
				g.Bold = true
			} else {
				g.Italic = true
			}
			out = append(out, c.inlines(t, g)...)
		case *extast.Strikethrough:
			g := f
			g.Strike = true
			out = append(out, c.inlines(t, g)...)
		case *ast.Link:
			out = append(out, c.link(string(t.Destination), c.inlines(t, f), f)...)
		case *ast.Image:
			out = append(out, c.link(string(t.Destination), c.inlines(t, f), f)...)
		case *ast.AutoLink:
			url := string(t.URL(c.src))
			label := string(t.Label(c.src))
			if t.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
				url = "mailto:" + url
			}
			out = append(out, c.link(url, []docx.Inline{{Text: label}}, f)...)
		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < t.Segments.Len(); i++ {
				seg := t.Segments.At(i)
				b.Write(seg.Value(c.src))
			}
			if brTag.MatchString(b.String()) {
				out = append(out, docx.Inline{Break: true})
			} else {
				add(b.String())
			}
		case *extast.TaskCheckBox:
			if t.IsChecked {
				add("[x] ")
			} else {
				add("[ ] ")
			}
		default:
			out = append(out, c.inlines(t, f)...)
		}
	}
	return out
}

// link makes label a hyperlink when url is one a reader can follow from a
// document. A relative URL is resolved against the base when there is one;
// without one it means nothing outside the site the spec was written for,
// so it is printed after the label instead.
func (c *mdConv) link(url string, label []docx.Inline, f docx.Inline) []docx.Inline {
	url = c.links.resolve(url)
	if !followable(url) {
		if url == "" || plain(label) == url {
			return label
		}
		in := f
		in.Text = " (" + url + ")"
		return append(label, in)
	}
	if len(label) == 0 {
		label = []docx.Inline{{Text: url}}
	}
	for i := range label {
		label[i].URL = url
	}
	return label
}

// linker resolves relative links against the base the user gave, and
// remembers the ones it could not, so the user can be told. There is no
// guessed base: the API's server is often not where its documentation
// lives, and a link to the wrong host looks exactly like a right one.
type linker struct {
	base       *url.URL
	unresolved []string
}

// resolve makes a relative link absolute against the base. A fragment-only
// link points into a page the manual is not, so it is left alone.
func (l *linker) resolve(link string) string {
	if link == "" || strings.HasPrefix(link, "#") || followable(link) {
		return link
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "" {
		return link
	}
	if l == nil {
		return link
	}
	if l.base == nil {
		if !slices.Contains(l.unresolved, link) {
			l.unresolved = append(l.unresolved, link)
		}
		return link
	}
	return l.base.ResolveReference(u).String()
}

func followable(url string) bool {
	u := strings.ToLower(url)
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "mailto:")
}

// unescape does what goldmark's HTML writer does to text on the way out:
// entity and numeric references become characters, and backslash escapes
// lose their backslash.
func unescape(b []byte) string {
	return string(util.UnescapePunctuations(util.ResolveNumericReferences(util.ResolveEntityNames(b))))
}

func plain(ins []docx.Inline) string {
	var b strings.Builder
	for _, in := range ins {
		if in.Break {
			b.WriteString(" ")
		}
		b.WriteString(in.Text)
	}
	return b.String()
}
