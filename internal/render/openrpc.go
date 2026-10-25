package render

import (
	"strconv"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/spec"
	"gopkg.in/yaml.v3"
)

var paramStructureText = map[string]string{
	"by-name":     "Params are sent by name, as an object.",
	"by-position": "Params are sent by position, as an array in the order below.",
	"either":      "Params may be sent by name, as an object, or by position, as an array in the order below.",
}

func (r *renderer) jsonrpc(op *spec.Operation) {
	m := op.JSONRPC
	r.doc.Heading(2, r.sectionTitle(op), r.ops[op])
	r.doc.Para(docx.Para{Style: "Endpoint", Inlines: []docx.Inline{{Text: "JSON-RPC", Bold: true}, {Text: "  " + op.ID}}})
	if op.Deprecated {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Deprecated. ", Bold: true}, {Text: "This method is still served but may be removed; avoid it in new code."}}})
	}
	r.write(r.markdown(op.Description))
	r.seeAlso(op.ExternalDocs)
	if len(m.Servers) > 0 {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Servers: ", Bold: true}, {Text: "this method is called at the following URLs, not at the document's servers."}}})
		r.serverTable(m.Servers)
	}

	if len(m.Params) == 0 {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "This method takes no params."}}})
	} else {
		r.doc.Heading(3, "Params", "")
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: paramStructureText[m.ParamStructure]}}})
		t := docx.Table{Widths: []int{1, 5, 5, 4, 8}, Header: []string{"#", "Name", "Type", "Required", "Description"}}
		for i, p := range m.Params {
			t.Rows = append(t.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: strconv.Itoa(i + 1)}}}},
				{{Inlines: []docx.Inline{{Text: p.Name, Code: true}}}},
				{{Inlines: r.typeOf(p.Schema)}},
				{{Inlines: []docx.Inline{{Text: yesNo(p.Required)}}}},
				r.cell(firstNonEmpty(p.Description, schemaDescription(p.Schema)), r.paramNotes(p)),
			})
		}
		r.doc.Table(t)
	}

	if m.Result == nil {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Result: ", Bold: true}, {Text: "none. This method is a notification: the server sends no response."}}})
	} else {
		in := append([]docx.Inline{{Text: "Result: ", Bold: true}}, r.typeOf(m.Result.Schema)...)
		r.doc.Para(docx.Para{Inlines: append(in, docx.Inline{Text: "."})})
		if notes := r.paramNotes(m.Result); len(notes) > 0 {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: strings.Join(notes, " ")}}})
		}
		r.write(r.markdown(firstNonEmpty(m.Result.Description, schemaDescription(m.Result.Schema))))
	}

	if len(m.Errors) > 0 {
		r.doc.Heading(3, "Errors", "")
		t := docx.Table{Widths: []int{3, 12}, Header: []string{"Code", "Message"}}
		data := false
		for _, e := range m.Errors {
			data = data || e.Data != nil
		}
		if data {
			t.Widths = []int{3, 7, 7}
			t.Header = append(t.Header, "Data")
		}
		for _, e := range m.Errors {
			row := []docx.Cell{
				{{Inlines: []docx.Inline{{Text: e.Code, Code: true}}}},
				{{Inlines: []docx.Inline{{Text: e.Message}}}},
			}
			if data {
				row = append(row, r.errorData(e.Data))
			}
			t.Rows = append(t.Rows, row)
		}
		r.doc.Table(t)
	}

	for _, x := range m.Examples {
		label := "Example"
		if l := firstNonEmpty(x.Summary, x.Name); l != "" {
			label += ": " + strings.TrimSuffix(l, ".")
		}
		r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: label}}})
		r.write(r.markdown(x.Description))
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Params:"}}})
		r.doc.Code(exampleJSON(exampleParams(x.Params, m.ParamStructure == "by-position")))
		for _, p := range x.Params {
			if p.Value == nil && p.External != "" {
				in := []docx.Inline{{Text: "The value of "}, {Text: p.Name, Code: true}, {Text: " is published at "}}
				r.doc.Para(docx.Para{Inlines: append(append(in, link(p.External, p.External)...), docx.Inline{Text: "."})})
			}
		}
		switch {
		case x.Result != nil:
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Result:"}}})
			r.doc.Code(exampleJSON(x.Result))
		case x.ResultExternal != "":
			in := []docx.Inline{{Text: "The result is published at "}}
			r.doc.Para(docx.Para{Inlines: append(append(in, link(x.ResultExternal, x.ResultExternal)...), docx.Inline{Text: "."})})
		}
	}
}

// errorData is an error's data member for a table cell: short values on
// one line, anything longer as a code block.
func (r *renderer) errorData(n *yaml.Node) docx.Cell {
	if n == nil {
		return docx.Cell{{}}
	}
	if v, ok := inlineExample(n); ok {
		return docx.Cell{{Inlines: []docx.Inline{{Text: v, Code: true}}}}
	}
	return docx.Cell{docx.CodePara(exampleJSON(n))}
}

func (r *renderer) paramNotes(p *spec.Parameter) []string {
	notes := r.notes(p.Schema)
	if p.Deprecated {
		notes = append([]string{"Deprecated."}, notes...)
	}
	return notes
}

// exampleParams builds the params member a client would send: an object,
// or an array when the method only takes params by position.
func exampleParams(params []spec.ParamValue, byPosition bool) *yaml.Node {
	if byPosition {
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, p := range params {
			n.Content = append(n.Content, orNull(p.Value))
		}
		return n
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range params {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p.Name}, orNull(p.Value))
	}
	return n
}

func orNull(n *yaml.Node) *yaml.Node {
	if n == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
	return n
}
