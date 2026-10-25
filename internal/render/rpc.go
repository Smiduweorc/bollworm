package render

import (
	"strconv"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/spec"
)

var streamingText = map[spec.Streaming]string{
	spec.Unary:           "none. The client sends one request and gets one response.",
	spec.ServerStreaming: "server. The client sends one request and reads a stream of responses.",
	spec.ClientStreaming: "client. The client sends a stream of requests and gets one response.",
	spec.BidiStreaming:   "bidirectional. The client and the server each send a stream, independently of each other.",
}

func (r *renderer) rpc(op *spec.Operation) {
	r.doc.Heading(2, r.sectionTitle(op), r.ops[op])
	r.doc.Para(docx.Para{Style: "Endpoint", Inlines: []docx.Inline{{Text: "RPC", Bold: true}, {Text: "  /" + op.RPC.Service + "/" + op.RPC.Method}}})
	if op.Deprecated {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Deprecated. ", Bold: true}, {Text: "This method is still served but may be removed; avoid it in new code."}}})
	}
	r.write(r.markdown(op.Description))

	message := func(label string, s *spec.Schema, stream bool) {
		in := []docx.Inline{{Text: label, Bold: true}}
		if stream {
			in = append(in, docx.Inline{Text: "a stream of "})
		}
		r.doc.Para(docx.Para{Inlines: append(append(in, r.typeOf(s)...), docx.Inline{Text: "."})})
	}
	message("Request: ", op.Input, op.Streaming == spec.ClientStreaming || op.Streaming == spec.BidiStreaming)
	message("Response: ", op.Output, op.Streaming == spec.ServerStreaming || op.Streaming == spec.BidiStreaming)
	r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Streaming: ", Bold: true}, {Text: streamingText[op.Streaming]}}})
	r.options(op.Options)

	if len(op.RPC.Bindings) == 0 {
		return
	}
	r.doc.Heading(3, "HTTP mapping", "")
	r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "A proxy that transcodes HTTP to gRPC, such as grpc-gateway or Envoy, accepts this method as:"}}})
	t := docx.Table{Widths: []int{2, 6, 4, 4}, Header: []string{"Method", "Path", "Request body", "Response body"}}
	for _, b := range op.RPC.Bindings {
		body := []docx.Inline{{Text: b.Body, Code: true}}
		switch b.Body {
		case "*":
			body = []docx.Inline{{Text: "The whole request"}}
		case "":
			body = []docx.Inline{{Text: "None; fields not in the path go in the query string"}}
		}
		resp := []docx.Inline{{Text: b.ResponseBody, Code: true}}
		if b.ResponseBody == "" {
			resp = []docx.Inline{{Text: "The whole response"}}
		}
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: b.Method}}}},
			{{Inlines: []docx.Inline{{Text: b.Path, Code: true}}}},
			{{Inlines: body}},
			{{Inlines: resp}},
		})
	}
	r.doc.Table(t)
}

// protoModel writes the body of a message's or an enum's section. Every
// nested message is a model of its own, so the fields table is one level.
func (r *renderer) protoModel(m *spec.Schema) {
	r.options(m.Options)
	if len(m.Values) > 0 {
		t := docx.Table{Widths: []int{6, 2, 9}, Header: []string{"Value", "Number", "Description"}}
		for _, v := range m.Values {
			var notes []string
			if v.Deprecated {
				notes = []string{"Deprecated."}
			}
			for _, o := range v.Options {
				notes = append(notes, "Option "+o+".")
			}
			t.Rows = append(t.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: v.Name, Code: true}}}},
				{{Inlines: []docx.Inline{{Text: strconv.Itoa(int(v.Number))}}}},
				r.cell(v.Description, notes),
			})
		}
		r.doc.Table(t)
		return
	}
	if len(m.Properties) == 0 {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "This message has no fields."}}})
		return
	}
	t := docx.Table{Widths: []int{5, 2, 6, 9}, Header: []string{"Field", "Number", "Type", "Description"}}
	for _, p := range m.Properties {
		notes := r.notes(p.Schema)
		if p.Required {
			notes = append([]string{"Required."}, notes...)
		}
		if p.Oneof != "" {
			notes = append(notes, "Part of oneof "+p.Oneof+": setting it clears the other fields of "+p.Oneof+".")
		}
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: p.Name, Code: true}}}},
			{{Inlines: []docx.Inline{{Text: strconv.Itoa(int(p.Number))}}}},
			{{Inlines: r.typeOf(p.Schema)}},
			r.cell(schemaDescription(p.Schema), notes),
		})
	}
	r.doc.Table(t)
}

// options lists protobuf options the manual has no words of its own for,
// such as validation rules, as they are written in the .proto file.
func (r *renderer) options(opts []string) {
	if len(opts) == 0 {
		return
	}
	r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Options:", Bold: true}}})
	id := r.doc.NewList(false, 1)
	for _, o := range opts {
		r.doc.Para(docx.Para{Style: "ListParagraph", List: &docx.ListItem{ID: id}, Inlines: []docx.Inline{{Text: o, Code: true}}})
	}
}
