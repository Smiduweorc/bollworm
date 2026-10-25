// Package render lays an OpenAPI document out as a reference manual: a title
// page, contents, an introduction, authentication, one chapter per tag and a
// chapter of data models. The layout decisions live here; how a paragraph or
// a table is spelled in WordprocessingML lives in package docx.
package render

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/spec"
	"gopkg.in/yaml.v3"
)

type Options struct {
	Paper docx.Paper
	// LinkBase is the URL relative links in descriptions are resolved
	// against. When it is nil, a relative link is printed after its text.
	LinkBase *url.URL
	// TOCDepth is the deepest heading level the contents list: 1 for
	// chapters, 2 (the default, when 0) for each endpoint, 3 for their
	// parts as well.
	TOCDepth int
	// Styles replaces the built-in styles part when it is not empty; see
	// docx.Reference.
	Styles string
}

// Render returns the .docx bytes for s, and warnings about what the
// document could not show as the spec wrote it.
func Render(s *spec.Spec, opts Options) ([]byte, []string, error) {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "API reference"
	}
	r := &renderer{
		spec:    s,
		doc:     docx.New(title, opts.Paper),
		models:  map[*spec.Schema]string{},
		schemes: map[string]string{},
		titles:  map[string]int{},
		ops:     map[*spec.Operation]string{},
		linker:  &linker{base: opts.LinkBase},
	}
	r.doc.Styles = opts.Styles
	if opts.TOCDepth > 0 {
		r.doc.TOCDepth = opts.TOCDepth
	}
	used := map[string]bool{}
	note := func(op *spec.Operation) {
		if r.ops[op] != "" {
			return
		}
		r.titles[operationTitle(op)]++
		r.ops[op] = anchor("Op_", firstNonEmpty(op.ID, operationTitle(op)), used)
		if op.HTTP == nil {
			return
		}
		// A callback's requests have no heading, but links may still
		// point at them.
		for _, cb := range op.HTTP.Callbacks {
			for _, cop := range cb.Operations {
				r.ops[cop] = anchor("Op_", firstNonEmpty(cop.ID, operationTitle(cop)), used)
			}
		}
	}
	for _, g := range s.Groups {
		for _, op := range g.Operations {
			note(op)
		}
	}
	for _, op := range s.Webhooks {
		note(op)
	}
	for _, m := range s.Models {
		r.models[m] = anchor("Model_", m.Name, used)
	}
	for _, sc := range s.Schemes {
		r.schemes[sc.Name] = anchor("Auth_", sc.Name, used)
	}

	r.titlePage(title)
	r.doc.Para(docx.Para{Style: "TOCHeading", Inlines: []docx.Inline{{Text: "Contents"}}})
	r.doc.TOC()
	r.introduction()
	r.authentication()
	for _, g := range s.Groups {
		r.group(g)
	}
	if len(s.Webhooks) > 0 {
		r.doc.Heading(1, "Webhooks", "")
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "These are requests the API sends to you. Each section describes the request your server receives and the responses it may return."}}})
		for _, op := range s.Webhooks {
			r.operation(op, true)
		}
	}
	r.dataModels()
	var warnings []string
	for _, l := range r.linker.unresolved {
		warnings = append(warnings, fmt.Sprintf("relative link %q is printed as text; pass --link-base to make it a link", l))
	}
	data, err := r.doc.Bytes()
	return data, warnings, err
}

type renderer struct {
	spec *spec.Spec
	doc  *docx.Doc
	// models maps each component schema to the bookmark of its section.
	models map[*spec.Schema]string
	// schemes maps each security scheme name to the bookmark of its section.
	schemes map[string]string
	// titles counts how many operations share each section title.
	titles map[string]int
	// ops maps each operation to the bookmark of its section.
	ops    map[*spec.Operation]string
	linker *linker
}

func (r *renderer) markdown(src string) []block {
	return markdown(r.doc, r.linker, src)
}

func (r *renderer) seeAlso(ed *spec.ExternalDocs) {
	if ed == nil {
		return
	}
	text := strings.TrimSuffix(strings.TrimSpace(ed.Description), ".")
	in := []docx.Inline{{Text: "See also: ", Bold: true}}
	if text != "" && text != ed.URL {
		in = append(in, link(text, ed.URL)...)
		in = append(in, docx.Inline{Text: " ("}, docx.Inline{Text: ed.URL, Code: true}, docx.Inline{Text: ")."})
	} else {
		in = append(in, link(ed.URL, ed.URL)...)
		in = append(in, docx.Inline{Text: "."})
	}
	r.doc.Para(docx.Para{Inlines: in})
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9_]`)

// anchor builds a bookmark name Word accepts: a letter first, only letters,
// digits and underscores, at most 40 characters, and unique in the document.
//
// A name too long to fit is cut and given a hash of the whole of it, so two
// long names that start alike still get different bookmarks, and the same
// name gets the same bookmark however the document around it changes.
func anchor(prefix, name string, used map[string]bool) string {
	base := prefix + nonWord.ReplaceAllString(name, "_")
	if len(base) > 40 {
		h := fnv.New32a()
		h.Write([]byte(name))
		base = fmt.Sprintf("%s_%08x", base[:31], h.Sum32())
	}
	a := base
	for i := 2; used[a]; i++ {
		suffix := "_" + strconv.Itoa(i)
		a = base[:min(len(base), 40-len(suffix))] + suffix
	}
	used[a] = true
	return a
}

func (r *renderer) titlePage(title string) {
	s := r.spec
	r.doc.Para(docx.Para{Style: "Title", Inlines: []docx.Inline{{Text: title}}})
	sub := "API reference"
	if s.APIVersion != "" {
		sub += ", version " + s.APIVersion
	}
	r.doc.Para(docx.Para{Style: "Subtitle", Inlines: []docx.Inline{{Text: sub}}})
	if s.Summary != "" {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: s.Summary}}})
	}

	if c := s.Contact; c.Name != "" || c.Email != "" || c.URL != "" {
		ins := []docx.Inline{{Text: "Contact: ", Bold: true}}
		var parts [][]docx.Inline
		if c.Name != "" {
			parts = append(parts, []docx.Inline{{Text: c.Name}})
		}
		if c.Email != "" {
			parts = append(parts, []docx.Inline{{Text: c.Email, URL: "mailto:" + c.Email}})
		}
		if c.URL != "" {
			parts = append(parts, link(c.URL, c.URL))
		}
		r.doc.Para(docx.Para{Inlines: append(ins, join(parts, ", ")...)})
	}
	if l := s.License; l.Name != "" {
		r.doc.Para(docx.Para{Inlines: append([]docx.Inline{{Text: "Licence: ", Bold: true}}, link(l.Name, l.URL)...)})
	}
	if s.TermsURL != "" {
		r.doc.Para(docx.Para{Inlines: append([]docx.Inline{{Text: "Terms of service: ", Bold: true}}, link(s.TermsURL, s.TermsURL)...)})
	}
}

func (r *renderer) introduction() {
	s := r.spec
	if strings.TrimSpace(s.Description) == "" && len(s.Servers) == 0 && len(s.Files) == 0 && s.ExternalDocs == nil {
		return
	}
	r.doc.Heading(1, "Introduction", "")
	r.write(r.markdown(s.Description))
	r.seeAlso(s.ExternalDocs)
	if len(s.Files) > 0 {
		r.doc.Heading(2, "Files", "")
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "This document describes the services and messages in these files."}}})
		t := docx.Table{Widths: []int{6, 5}, Header: []string{"File", "Package"}}
		for _, f := range s.Files {
			t.Rows = append(t.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: f.Name, Code: true}}}},
				{{Inlines: []docx.Inline{{Text: f.Package, Code: true}}}},
			})
		}
		r.doc.Table(t)
	}
	if len(s.Servers) == 0 {
		return
	}
	r.doc.Heading(2, "Servers", "")
	switch {
	case s.Format == spec.OpenRPC && len(s.Servers) == 1:
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Every method is called at the following URL."}}})
	case s.Format == spec.OpenRPC:
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Every method can be called at any of the following URLs."}}})
	case len(s.Servers) == 1:
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Requests are made to the following base URL. Paths in this document are relative to it."}}})
	default:
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Requests can be made to any of the following base URLs. Paths in this document are relative to them."}}})
	}
	r.serverTable(s.Servers)
}

func (r *renderer) serverTable(servers []spec.Server) {
	t := docx.Table{Widths: []int{5, 4}, Header: []string{"URL", "Description"}}
	for _, sv := range servers {
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: sv.URL, Code: true}}}},
			r.cell(sv.Description, nil),
		})
	}
	r.doc.Table(t)
	r.serverVariables(servers)
}

func (r *renderer) serverVariables(servers []spec.Server) {
	for _, sv := range servers {
		if len(sv.Variables) == 0 {
			continue
		}
		r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: "Variables in "}, {Text: sv.URL, Code: true}}})
		vt := docx.Table{Widths: []int{2, 2, 3, 4}, Header: []string{"Variable", "Default", "Allowed values", "Description"}}
		for _, v := range sv.Variables {
			var allowed []docx.Inline
			for i, e := range v.Enum {
				if i > 0 {
					allowed = append(allowed, docx.Inline{Text: ", "})
				}
				allowed = append(allowed, docx.Inline{Text: e, Code: true})
			}
			if len(allowed) == 0 {
				allowed = []docx.Inline{{Text: "Any"}}
			}
			vt.Rows = append(vt.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: v.Name, Code: true}}}},
				{{Inlines: []docx.Inline{{Text: v.Default, Code: true}}}},
				{{Inlines: allowed}},
				r.cell(v.Description, nil),
			})
		}
		r.doc.Table(vt)
	}
}

func (r *renderer) authentication() {
	s := r.spec
	if len(s.Schemes) == 0 {
		return
	}
	r.doc.Heading(1, "Authentication", "")
	if len(s.Security) > 0 {
		r.doc.Para(docx.Para{Inlines: append([]docx.Inline{{Text: "Unless an endpoint says otherwise, requests are authenticated with "}}, append(r.requirement(s.Security), docx.Inline{Text: "."})...)})
	}
	for _, sc := range s.Schemes {
		r.doc.Heading(2, sc.Name, r.schemes[sc.Name])
		r.doc.Para(docx.Para{Inlines: schemeSummary(sc)})
		r.write(r.markdown(sc.Description))
		if len(sc.Flows) == 0 {
			continue
		}
		refresh := slices.ContainsFunc(sc.Flows, func(f spec.OAuthFlow) bool { return f.RefreshURL != "" })
		ft := docx.Table{Widths: []int{3, 5, 5}, Header: []string{"Flow", "Authorization URL", "Token URL"}}
		if refresh {
			ft.Widths = append(ft.Widths, 5)
			ft.Header = append(ft.Header, "Refresh URL")
		}
		var scopes []spec.Scope
		for _, f := range sc.Flows {
			row := []docx.Cell{
				{{Inlines: []docx.Inline{{Text: flowName(f.Kind)}}}},
				{{Inlines: link(f.AuthorizationURL, f.AuthorizationURL)}},
				{{Inlines: link(f.TokenURL, f.TokenURL)}},
			}
			if refresh {
				row = append(row, docx.Cell{{Inlines: link(f.RefreshURL, f.RefreshURL)}})
			}
			ft.Rows = append(ft.Rows, row)
			for _, scope := range f.Scopes {
				if !slices.ContainsFunc(scopes, func(x spec.Scope) bool { return x.Name == scope.Name }) {
					scopes = append(scopes, scope)
				}
			}
		}
		r.doc.Table(ft)
		if len(scopes) > 0 {
			r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: "Scopes"}}})
			st := docx.Table{Widths: []int{4, 7}, Header: []string{"Scope", "Description"}}
			for _, scope := range scopes {
				st.Rows = append(st.Rows, []docx.Cell{
					{{Inlines: []docx.Inline{{Text: scope.Name, Code: true}}}},
					r.cell(scope.Description, nil),
				})
			}
			r.doc.Table(st)
		}
	}
}

func schemeSummary(sc *spec.SecurityScheme) []docx.Inline {
	switch sc.Type {
	case "apiKey":
		where := map[string]string{"header": "header", "query": "query parameter", "cookie": "cookie"}[sc.In]
		if where == "" {
			where = sc.In
		}
		return []docx.Inline{{Text: "An API key sent in the "}, {Text: sc.ParamName, Code: true}, {Text: " " + where + "."}}
	case "http":
		switch strings.ToLower(sc.HTTPScheme) {
		case "bearer":
			s := "A bearer token sent in the Authorization header"
			if sc.BearerFormat != "" {
				s += " (" + sc.BearerFormat + ")"
			}
			return []docx.Inline{{Text: s + "."}}
		case "basic":
			return []docx.Inline{{Text: "HTTP Basic authentication: a user name and password sent in the Authorization header."}}
		default:
			return []docx.Inline{{Text: "HTTP authentication with the "}, {Text: sc.HTTPScheme, Code: true}, {Text: " scheme."}}
		}
	case "oauth2":
		if len(sc.Flows) == 1 {
			return []docx.Inline{{Text: "OAuth 2.0, using the flow below."}}
		}
		return []docx.Inline{{Text: "OAuth 2.0, using any of the flows below."}}
	case "openIdConnect":
		return append([]docx.Inline{{Text: "OpenID Connect, configured from "}}, append(link(sc.OpenIDConnectURL, sc.OpenIDConnectURL), docx.Inline{Text: "."})...)
	case "mutualTLS":
		return []docx.Inline{{Text: "Mutual TLS: the client presents a certificate when it connects."}}
	}
	return []docx.Inline{{Text: "Authentication of type "}, {Text: sc.Type, Code: true}, {Text: "."}}
}

func flowName(kind string) string {
	switch kind {
	case "authorizationCode":
		return "Authorization code"
	case "clientCredentials":
		return "Client credentials"
	case "password":
		return "Password"
	case "implicit":
		return "Implicit"
	case "deviceAuthorization":
		return "Device authorization"
	}
	return kind
}

// requirement describes a security requirement as a phrase: the
// alternatives joined by "or", the schemes within one by "together with",
// so "A together with B, or C" cannot be read two ways.
func (r *renderer) requirement(reqs []spec.Requirement) []docx.Inline {
	var alts [][]docx.Inline
	for _, req := range reqs {
		if len(req) == 0 {
			alts = append(alts, []docx.Inline{{Text: "no authentication"}})
			continue
		}
		var uses [][]docx.Inline
		for _, u := range req {
			in := []docx.Inline{{Text: u.Name, Anchor: r.schemes[u.Name]}}
			if len(u.Scopes) > 0 {
				in = append(in, docx.Inline{Text: " (scopes "})
				for i, sc := range u.Scopes {
					if i > 0 {
						in = append(in, docx.Inline{Text: ", "})
					}
					in = append(in, docx.Inline{Text: sc, Code: true})
				}
				in = append(in, docx.Inline{Text: ")"})
			}
			uses = append(uses, in)
		}
		alts = append(alts, join(uses, " together with "))
	}
	return join(alts, ", or ")
}

func (r *renderer) group(g spec.Group) {
	name := g.Name
	if name == "" {
		name = "Endpoints"
		if r.spec.Format == spec.OpenRPC {
			name = "Methods"
		}
		if len(r.spec.Groups) > 1 {
			name = "Other " + strings.ToLower(name)
		}
	}
	r.doc.Heading(1, name, "")
	r.write(r.markdown(g.Description))
	r.seeAlso(g.ExternalDocs)
	if len(g.Also) > 0 {
		what := "endpoints"
		if r.spec.Format == spec.OpenRPC {
			what = "methods"
		}
		if len(g.Operations) > 0 {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "These " + what + " belong here too, and are described in the chapter of their first tag:"}}})
		} else {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "These " + what + " belong here, and are described in the chapter of their first tag:"}}})
		}
		id := r.doc.NewList(false, 1)
		for _, op := range g.Also {
			r.doc.Para(docx.Para{Style: "ListParagraph", List: &docx.ListItem{ID: id}, Inlines: []docx.Inline{{Text: r.sectionTitle(op), Anchor: r.ops[op]}}})
		}
	}
	for _, op := range g.Operations {
		r.operation(op, false)
	}
}

// sectionTitle is the heading an operation's section gets: its title, told
// apart from others with the same one.
func (r *renderer) sectionTitle(op *spec.Operation) string {
	title := operationTitle(op)
	if r.titles[title] <= 1 {
		return title
	}
	switch {
	case op.HTTP != nil:
		return title + " (" + op.HTTP.Method + ")"
	case op.RPC != nil:
		service := op.RPC.Service
		return title + " (" + service[strings.LastIndex(service, ".")+1:] + ")"
	}
	return title + " (" + op.ID + ")"
}

func operationTitle(op *spec.Operation) string {
	title := op.Summary
	if title == "" {
		title = op.ID
	}
	if title == "" && op.HTTP != nil {
		title = op.HTTP.Method + " " + op.HTTP.Path
	}
	title = strings.TrimSpace(title)
	// A summary written as a sentence loses its full stop, as headings do;
	// an ellipsis or an abbreviation such as "etc." keeps it.
	if strings.HasSuffix(title, ".") && !strings.HasSuffix(title, "..") && !abbreviation.MatchString(title) {
		title = strings.TrimSuffix(title, ".")
	}
	return title
}

// abbreviation matches a title ending in a short abbreviation: "etc.",
// "e.g.", "Inc.", "v2.".
var abbreviation = regexp.MustCompile(`(?:^|[\s(])(?:etc|e\.g|i\.e|vs|approx|Inc|Ltd|Co|Corp|[A-Za-z]|v\d+)\.$`)

// operation writes one endpoint. For a webhook, Path is the webhook's name
// rather than a path on the API.
func (r *renderer) operation(op *spec.Operation, webhook bool) {
	if op.RPC != nil {
		r.rpc(op)
		return
	}
	if op.JSONRPC != nil {
		r.jsonrpc(op)
		return
	}
	ep := op.HTTP
	// Two endpoints with one summary would be two identical lines in the
	// contents; the method tells them apart.
	r.doc.Heading(2, r.sectionTitle(op), r.ops[op])
	if webhook {
		r.doc.Para(docx.Para{Style: "Endpoint", Inlines: []docx.Inline{{Text: ep.Method, Bold: true}, {Text: "  to your webhook URL"}}})
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Webhook: ", Bold: true}, {Text: ep.Path, Code: true}}})
	} else {
		r.doc.Para(docx.Para{Style: "Endpoint", Inlines: []docx.Inline{{Text: ep.Method, Bold: true}, {Text: "  " + ep.Path}}})
	}
	r.endpointBody(op, func(text string) { r.doc.Heading(3, text, "") }, !webhook)

	for _, cb := range ep.Callbacks {
		r.doc.Heading(3, "Callback: "+cb.Name, "")
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "The API sends this request to a URL taken from the call above, which your server receives."}}})
		for _, cop := range cb.Operations {
			in := []docx.Inline{{Text: cop.HTTP.Method, Bold: true}, {Text: "  " + cop.HTTP.Path}}
			if t := operationTitle(cop); cop.Summary != "" || cop.ID != "" {
				in = append([]docx.Inline{{Text: t + ": ", Bold: true}}, in...)
			}
			r.doc.Para(docx.Para{Style: "Endpoint", Inlines: in, Bookmark: r.ops[cop]})
			r.endpointBody(cop, func(text string) { r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: text}}}) }, false)
		}
	}
}

// endpointBody writes what follows an endpoint's method and path. sub
// writes a subsection title: a heading in an operation's own section, a
// label inside a callback. auth says whether the document's schemes apply.
func (r *renderer) endpointBody(op *spec.Operation, sub func(string), auth bool) {
	ep := op.HTTP
	if op.Deprecated {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Deprecated. ", Bold: true}, {Text: "This endpoint is still served but may be removed; avoid it in new code."}}})
	}
	r.write(r.markdown(op.Description))
	r.seeAlso(op.ExternalDocs)

	if ep.Security != nil || len(r.spec.Schemes) > 0 && auth {
		in := []docx.Inline{{Text: "Authentication: ", Bold: true}}
		if len(ep.Security) == 0 {
			in = append(in, docx.Inline{Text: "none."})
		} else {
			in = append(in, r.requirement(ep.Security)...)
			in = append(in, docx.Inline{Text: "."})
		}
		r.doc.Para(docx.Para{Inlines: in})
	}

	if len(ep.Parameters) > 0 {
		sub("Parameters")
		t := docx.Table{Widths: []int{5, 3, 5, 4, 8}, Header: []string{"Name", "In", "Type", "Required", "Description"}}
		for _, p := range ep.Parameters {
			notes := r.notes(p.Schema)
			if p.Deprecated {
				notes = append([]string{"Deprecated."}, notes...)
			}
			if p.Example != nil {
				if ex, ok := inlineExample(p.Example); ok {
					notes = append(notes, "Example: "+ex+".")
				}
			}
			if p.ContentType != "" {
				notes = append(notes, "Sent as "+p.ContentType+".")
			}
			if ser := serialization(p.Style, p.Explode); ser != "" {
				notes = append(notes, upperFirst(ser)+".")
			}
			t.Rows = append(t.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: p.Name, Code: true}}}},
				{{Inlines: []docx.Inline{{Text: p.In}}}},
				{{Inlines: r.typeOf(p.Schema)}},
				{{Inlines: []docx.Inline{{Text: yesNo(p.Required)}}}},
				r.cell(firstNonEmpty(p.Description, schemaDescription(p.Schema)), notes),
			})
		}
		r.doc.Table(t)
	}

	if b := ep.RequestBody; b != nil {
		sub("Request body")
		if b.Required {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "A request body is required."}}})
		} else {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "The request body is optional."}}})
		}
		r.write(r.markdown(b.Description))
		for _, c := range mergeContent(b.Content) {
			r.content(c, "Example request body")
		}
	}

	if len(ep.Responses) > 0 {
		sub("Responses")
		t := docx.Table{Widths: []int{3, 7, 5}, Header: []string{"Status", "Description", "Body"}}
		for _, resp := range ep.Responses {
			var notes []string
			for _, h := range resp.Headers {
				notes = append(notes, headerNote(h))
			}
			var body []docx.Inline
			for i, c := range mergeContent(resp.Content) {
				if i > 0 {
					body = append(body, docx.Inline{Break: true})
				}
				if c.schema != nil {
					body = append(body, r.typeOf(c.schema)...)
					body = append(body, docx.Inline{Text: " "})
				}
				body = append(body, docx.Inline{Text: "(" + strings.Join(c.types, ", ") + ")"})
			}
			if len(body) == 0 {
				body = []docx.Inline{{Text: "None"}}
			}
			t.Rows = append(t.Rows, []docx.Cell{
				{{Inlines: []docx.Inline{{Text: statusText(resp.Status)}}}},
				r.cell(resp.Description, notes),
				{{Inlines: body}},
			})
		}
		r.doc.Table(t)
		for _, resp := range ep.Responses {
			contents := mergeContent(resp.Content)
			for _, c := range contents {
				label := "Example " + resp.Status + " response"
				if len(contents) > 1 {
					label += " (" + strings.Join(c.types, ", ") + ")"
				}
				r.responseDetail(resp.Status, label, c)
			}
			r.links(resp)
		}
	}
}

func headerNote(h *spec.Header) string {
	note := "Header " + h.Name
	var about []string
	if t := plainType(h.Schema); t != "" {
		about = append(about, t)
	}
	if h.Required {
		about = append(about, "always sent")
	}
	if len(about) > 0 {
		note += " (" + strings.Join(about, ", ") + ")"
	}
	if h.Description != "" {
		note += ": " + strings.TrimSuffix(h.Description, ".")
	}
	return note + "."
}

// linkValue is a link's value as the spec wrote it: a runtime expression or
// a string as it is, anything else as JSON in the author's key order.
func linkValue(n *yaml.Node) []docx.Inline {
	n = deref(n)
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return []docx.Inline{{Text: n.Value, Code: true}}
	}
	if v, ok := inlineExample(n); ok {
		return []docx.Inline{{Text: v, Code: true}}
	}
	// Too long for a compact line: the indented JSON, folded onto one.
	lines := strings.Split(strings.TrimSpace(exampleJSON(n)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return []docx.Inline{{Text: strings.Join(lines, " "), Code: true}}
}

// serialization describes a parameter's or a form field's style and
// explode, or returns "" when the spec leaves both to their defaults.
func serialization(style string, explode *bool) string {
	var parts []string
	if style != "" {
		parts = append(parts, "serialized with style "+style)
	}
	if explode != nil {
		if *explode {
			parts = append(parts, "exploded: each value or property is sent as its own parameter")
		} else {
			parts = append(parts, "not exploded: values are sent comma-separated in one parameter")
		}
	}
	return strings.Join(parts, ", ")
}

func (r *renderer) links(resp *spec.Response) {
	if len(resp.Links) == 0 {
		return
	}
	r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: "Links from the " + resp.Status + " response"}}})
	t := docx.Table{Widths: []int{4, 5, 7, 6}, Header: []string{"Link", "Operation", "Values passed", "Description"}}
	for _, l := range resp.Links {
		target := []docx.Inline{{Text: firstNonEmpty(l.OperationID, l.OperationRef), Code: true}}
		if l.Target != nil {
			target = []docx.Inline{{Text: r.sectionTitle(l.Target), Anchor: r.ops[l.Target]}}
		}
		var values docx.Cell
		for _, p := range l.Parameters {
			values = append(values, docx.Para{Inlines: append([]docx.Inline{{Text: p.Name, Code: true}, {Text: " = "}}, linkValue(p.Value)...)})
		}
		if l.RequestBody != nil {
			values = append(values, docx.Para{Inlines: append([]docx.Inline{{Text: "Request body = "}}, linkValue(l.RequestBody)...)})
		}
		if len(values) == 0 {
			values = docx.Cell{{Inlines: []docx.Inline{{Text: "None"}}}}
		}
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: l.Name}}}},
			{{Inlines: target}},
			values,
			r.cell(l.Description, nil),
		})
	}
	r.doc.Table(t)
}

// responseDetail prints what the responses table cannot hold: the fields of
// a body that is not a named model, and the examples.
func (r *renderer) responseDetail(status, label string, c content) {
	if c.schema != nil && r.modelOf(c.schema) == nil && len(r.fields(c.schema)) > 0 {
		r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: "Fields of the " + status + " response"}}})
		r.fieldTable(c.schema)
	}
	r.examples(c, label)
}

// content describes a request body: its type in a sentence, then its fields
// when it is not a named model, then its examples.
func (r *renderer) content(c content, label string) {
	ct := []docx.Inline{{Text: "Content type: "}}
	for i, t := range c.types {
		if i > 0 {
			ct = append(ct, docx.Inline{Text: ", "})
		}
		ct = append(ct, docx.Inline{Text: t, Code: true})
	}
	table := c.schema != nil && r.modelOf(c.schema) == nil && len(r.fields(c.schema)) > 0
	switch {
	case table:
		ct = append(ct, docx.Inline{Text: ". The body is an object with these fields:"})
	case c.schema != nil:
		ct = append(ct, docx.Inline{Text: ". Body: "})
		ct = append(ct, r.typeOf(c.schema)...)
		ct = append(ct, docx.Inline{Text: "."})
	default:
		ct = append(ct, docx.Inline{Text: "."})
	}
	r.doc.Para(docx.Para{Inlines: ct})
	if table {
		r.fieldTable(c.schema)
	}
	r.encoding(c.encoding)
	r.examples(c, label)
}

// encoding describes how the fields of a form or multipart body are sent.
func (r *renderer) encoding(encs []spec.Encoding) {
	if len(encs) == 0 {
		return
	}
	r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: "How fields are sent"}}})
	t := docx.Table{Widths: []int{4, 5, 8}, Header: []string{"Field", "Content type", "Notes"}}
	for _, e := range encs {
		var notes []string
		if ser := serialization(e.Style, e.Explode); ser != "" {
			notes = append(notes, upperFirst(ser)+".")
		}
		for _, h := range e.Headers {
			notes = append(notes, headerNote(h))
		}
		ct := []docx.Inline{{Text: "Default"}}
		if e.ContentType != "" {
			ct = []docx.Inline{{Text: e.ContentType, Code: true}}
		}
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: e.Property, Code: true}}}},
			{{Inlines: ct}},
			{{Inlines: []docx.Inline{{Text: strings.Join(notes, " ")}}}},
		})
	}
	r.doc.Table(t)
}

func (r *renderer) examples(c content, label string) {
	for _, ex := range c.examples {
		l := label
		if ex.Summary != "" {
			l += ": " + strings.TrimSuffix(ex.Summary, ".")
		}
		r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: l}}})
		if ex.Value == nil {
			if ex.External != "" {
				r.doc.Para(docx.Para{Inlines: append([]docx.Inline{{Text: "Published at "}}, link(ex.External, ex.External)...)})
			}
			continue
		}
		v := deref(ex.Value)
		if v.Kind == 0 {
			continue
		}
		// A string example for a type that is not JSON (XML, CSV, plain
		// text) is the body itself, not a JSON string.
		if v.ShortTag() == "!!str" && !isJSON(c.types) {
			r.doc.Code(v.Value)
		} else {
			r.doc.Code(exampleJSON(v))
		}
	}
}

func isJSON(types []string) bool {
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(strings.SplitN(t, ";", 2)[0]))
		if t == "application/json" || strings.HasSuffix(t, "+json") || t == "*/*" {
			return true
		}
	}
	return false
}

// content is one or more media types that share a schema and examples.
type content struct {
	types    []string
	schema   *spec.Schema
	examples []spec.Example
	encoding []spec.Encoding
}

// mergeContent folds media types with the same schema into one entry, so an
// endpoint accepting JSON and form data with one schema says so once.
func mergeContent(mts []*spec.MediaType) []content {
	var out []content
	for _, mt := range mts {
		merged := false
		for i := range out {
			if out[i].schema == mt.Schema && len(mt.Examples) == 0 && len(mt.Encoding) == 0 {
				out[i].types = append(out[i].types, mt.Type)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, content{types: []string{mt.Type}, schema: mt.Schema, examples: mt.Examples, encoding: mt.Encoding})
		}
	}
	return out
}

func statusText(code string) string {
	n, err := strconv.Atoi(code)
	if err != nil {
		if code == "default" {
			return "Any other"
		}
		// OpenAPI's status ranges: 1XX to 5XX.
		classes := map[string]string{"1": "Informational", "2": "Success", "3": "Redirection", "4": "Client error", "5": "Server error"}
		if len(code) == 3 && strings.EqualFold(code[1:], "XX") && classes[code[:1]] != "" {
			return code + " " + classes[code[:1]]
		}
		return code
	}
	if t := http.StatusText(n); t != "" {
		return code + " " + t
	}
	return code
}

func (r *renderer) dataModels() {
	if len(r.spec.Models) == 0 {
		return
	}
	r.doc.Heading(1, "Data models", "")
	if r.proto() {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "These are the messages and enums that requests and responses carry. Types from google.protobuf are the well-known types and are shown by name."}}})
	} else {
		r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "These are the objects that requests and responses carry. A field marked as required is always present."}}})
	}
	for _, m := range r.spec.Models {
		r.doc.Heading(2, m.Name, r.models[m])
		if m.Deprecated {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "Deprecated.", Bold: true}}})
		}
		r.write(r.markdown(m.Description))
		r.seeAlso(m.ExternalDocs)
		if r.proto() {
			r.protoModel(m)
			continue
		}

		alts, lead := m.OneOf, " is one of the following:"
		if len(alts) == 0 {
			alts, lead = m.AnyOf, " matches at least one of the following:"
		}
		if len(alts) > 0 {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: m.Name + lead}}})
			id := r.doc.NewList(false, 1)
			for _, a := range alts {
				r.doc.Para(docx.Para{Style: "ListParagraph", List: &docx.ListItem{ID: id}, Inlines: r.typeOf(a)})
			}
		}
		r.discriminator(m)
		if x := xmlNote(m.XML); x != "" {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: x}}})
		}

		if len(r.fields(m)) > 0 {
			r.fieldTable(m)
		} else if len(alts) == 0 {
			in := append([]docx.Inline{{Text: "Type: "}}, r.typeOfInner(m)...)
			in = append(in, docx.Inline{Text: "."})
			if notes := details(m); len(notes) > 0 {
				in = append(in, docx.Inline{Text: " " + strings.Join(notes, " ")})
			}
			r.doc.Para(docx.Para{Inlines: in})
		}
		if m.Closed && len(r.fields(m)) > 0 {
			r.doc.Para(docx.Para{Inlines: []docx.Inline{{Text: "No properties other than these are allowed."}}})
		}
		for i, ex := range m.Examples {
			label := "Example"
			if len(m.Examples) > 1 {
				label += " " + strconv.Itoa(i+1)
			}
			r.doc.Para(docx.Para{Style: "Label", Inlines: []docx.Inline{{Text: label}}})
			r.doc.Code(exampleJSON(ex))
		}
	}
}

// discriminator says which property tells a model's alternatives apart.
func (r *renderer) discriminator(m *spec.Schema) {
	d := m.Discriminator
	if d == nil || d.Property == "" {
		return
	}
	in := []docx.Inline{{Text: "The "}, {Text: d.Property, Code: true}, {Text: " property says which one a value is"}}
	if len(d.Mapping) == 0 {
		r.doc.Para(docx.Para{Inlines: append(in, docx.Inline{Text: ": it holds the name of the model."})})
		return
	}
	r.doc.Para(docx.Para{Inlines: append(in, docx.Inline{Text: ":"})})
	t := docx.Table{Widths: []int{4, 6}, Header: []string{"Value of " + d.Property, "Model"}}
	for _, c := range d.Mapping {
		model := []docx.Inline{{Text: c.Ref, Code: true}}
		if c.Schema != nil {
			model = r.typeOf(c.Schema)
		}
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: c.Value, Code: true}}}},
			{{Inlines: model}},
		})
	}
	r.doc.Table(t)
}

// xmlNote describes how a value is spelled in an XML body, or returns "".
func xmlNote(x *spec.XML) string {
	if x == nil {
		return ""
	}
	var parts []string
	switch {
	case x.Attribute && x.Name != "":
		parts = append(parts, "an attribute named "+x.Name)
	case x.Attribute:
		parts = append(parts, "an attribute")
	case x.Name != "":
		parts = append(parts, "an element named "+x.Name)
	}
	if x.Wrapped {
		parts = append(parts, "wrapped in an outer element")
	}
	if x.Prefix != "" {
		parts = append(parts, "with the prefix "+x.Prefix)
	}
	if x.Namespace != "" {
		parts = append(parts, "in the namespace "+x.Namespace)
	}
	if len(parts) == 0 {
		return ""
	}
	return "In XML, " + strings.Join(parts, ", ") + "."
}

// field is one row of a fields table. Name is a path: nested objects that
// have no model of their own are spelled out as "address.city" so the reader
// sees the whole shape in one table.
type field struct {
	name     string
	required bool
	schema   *spec.Schema
}

// maxFieldDepth stops spelling out anonymous nested objects. Past this, the
// row says "object" and the reader has the shape from the example.
const maxFieldDepth = 4

func (r *renderer) fields(s *spec.Schema) []field {
	var out []field
	r.collect(s, "", 0, map[*spec.Schema]bool{}, &out)
	return out
}

func (r *renderer) collect(s *spec.Schema, prefix string, depth int, onPath map[*spec.Schema]bool, out *[]field) {
	if onPath[s] {
		return
	}
	onPath[s] = true
	defer delete(onPath, s)
	for _, p := range properties(s) {
		name := prefix + p.Name
		*out = append(*out, field{name, p.Required, p.Schema})
		if depth+1 >= maxFieldDepth {
			continue
		}
		inner, suffix := p.Schema, "."
		if inner != nil && slices.Contains(inner.Types, "array") && inner.Items != nil {
			inner, suffix = inner.Items, "[]."
		}
		if inner != nil && r.modelOf(inner) == nil && len(properties(inner)) > 0 {
			r.collect(inner, name+suffix, depth+1, onPath, out)
		}
	}
}

// properties returns s's own properties and those it takes in through allOf,
// in the order they are declared. A property declared twice keeps its first
// position and is required if either declaration says so.
func properties(s *spec.Schema) []*spec.Property {
	var out []*spec.Property
	seen := map[string]int{}
	var walk func(*spec.Schema, map[*spec.Schema]bool)
	walk = func(s *spec.Schema, onPath map[*spec.Schema]bool) {
		if s == nil || onPath[s] {
			return
		}
		onPath[s] = true
		defer delete(onPath, s)
		for _, a := range s.AllOf {
			walk(a, onPath)
		}
		for _, p := range s.Properties {
			if i, ok := seen[p.Name]; ok {
				if p.Required && !out[i].Required {
					cp := *out[i]
					cp.Required = true
					out[i] = &cp
				}
				continue
			}
			seen[p.Name] = len(out)
			out = append(out, p)
		}
	}
	walk(s, map[*spec.Schema]bool{})
	return out
}

func (r *renderer) fieldTable(s *spec.Schema) {
	t := docx.Table{Widths: []int{5, 5, 4, 8}, Header: []string{"Field", "Type", "Required", "Description"}}
	for _, f := range r.fields(s) {
		t.Rows = append(t.Rows, []docx.Cell{
			{{Inlines: []docx.Inline{{Text: f.name, Code: true}}}},
			{{Inlines: r.typeOf(f.schema)}},
			{{Inlines: []docx.Inline{{Text: yesNo(f.required)}}}},
			r.cell(schemaDescription(f.schema), r.notes(f.schema)),
		})
	}
	r.doc.Table(t)
}

// schemaDescription is the description a field shows: its own, or its title.
// A field that is just a model shows none, because the type column links to
// the model's own section.
func schemaDescription(s *spec.Schema) string {
	if s == nil || s.Name != "" {
		return ""
	}
	if s.Description == "" && len(s.AllOf) == 1 && len(s.Properties) == 0 && s.AllOf[0].Name == "" {
		return s.AllOf[0].Description
	}
	return firstNonEmpty(s.Description, s.Title)
}

// typeOf names a schema's type for a table cell. A model links to its
// section; everything else is described in words.
func (r *renderer) typeOf(s *spec.Schema) []docx.Inline {
	if s == nil {
		return []docx.Inline{{Text: "any"}}
	}
	if m := r.modelOf(s); m != nil && !slices.Contains(s.Types, "null") {
		return []docx.Inline{{Text: m.Name, Anchor: r.models[m]}}
	}
	return r.typeOfInner(s)
}

// modelOf returns the model s is, or nil. "allOf: [$ref]" with nothing
// beside it is how 3.0 attaches a description to a reference, and it is
// still that model.
func (r *renderer) modelOf(s *spec.Schema) *spec.Schema {
	for s != nil {
		if r.models[s] != "" {
			return s
		}
		if len(s.AllOf) != 1 || len(s.Properties) > 0 || len(s.Types) > 0 || len(s.OneOf) > 0 || len(s.AnyOf) > 0 {
			return nil
		}
		s = s.AllOf[0]
	}
	return nil
}

// typeOfInner describes s without linking s itself, which is what a model's
// own section needs.
func (r *renderer) typeOfInner(s *spec.Schema) []docx.Inline {
	if s.Never {
		return []docx.Inline{{Text: "nothing (no value is allowed)"}}
	}
	nullable := slices.Contains(s.Types, "null")
	var out []docx.Inline
	switch {
	case len(s.AllOf) == 1 && len(s.Properties) == 0 && (len(s.Types) == 0 || nullable && len(s.Types) == 1):
		out = r.typeOf(s.AllOf[0])
	case len(s.OneOf) > 0:
		out = r.alternatives("one of ", s.OneOf, " or ")
	case len(s.AnyOf) > 0:
		out = r.alternatives("any of ", s.AnyOf, " or ")
	case len(s.AllOf) > 1 && len(s.Properties) == 0:
		out = r.alternatives("all of ", s.AllOf, " and ")
	case s.Const != nil:
		v, _ := inlineExample(s.Const)
		out = []docx.Inline{{Text: "constant "}, {Text: v, Code: true}}
	default:
		types := slices.DeleteFunc(slices.Clone(s.Types), func(t string) bool { return t == "null" })
		if len(types) == 0 {
			switch {
			case len(properties(s)) > 0:
				types = []string{"object"}
			case s.Items != nil:
				types = []string{"array"}
			default:
				if nullable {
					return []docx.Inline{{Text: "null"}}
				}
				return []docx.Inline{{Text: "any"}}
			}
		}
		for i, t := range types {
			if i > 0 {
				out = append(out, docx.Inline{Text: " or "})
			}
			out = append(out, r.single(s, t)...)
		}
	}
	if nullable {
		out = append(out, docx.Inline{Text: " or null"})
	}
	return out
}

func (r *renderer) single(s *spec.Schema, t string) []docx.Inline {
	switch t {
	case "array":
		if s.Items == nil {
			return []docx.Inline{{Text: "array"}}
		}
		if r.proto() {
			return append([]docx.Inline{{Text: "repeated "}}, r.typeOf(s.Items)...)
		}
		return append([]docx.Inline{{Text: "array of "}}, r.typeOf(s.Items)...)
	case "object":
		if s.Key != nil && s.AdditionalProperties != nil {
			out := append([]docx.Inline{{Text: "map<"}}, r.typeOf(s.Key)...)
			out = append(append(out, docx.Inline{Text: ", "}), r.typeOf(s.AdditionalProperties)...)
			return append(out, docx.Inline{Text: ">"})
		}
		if s.AdditionalProperties != nil && len(s.Properties) == 0 {
			return append([]docx.Inline{{Text: "map of "}}, r.typeOf(s.AdditionalProperties)...)
		}
		return []docx.Inline{{Text: "object"}}
	}
	if s.Format != "" {
		return []docx.Inline{{Text: t + " (" + s.Format + ")"}}
	}
	return []docx.Inline{{Text: t}}
}

// proto is false for the renderer plainType builds, which has no spec.
func (r *renderer) proto() bool {
	return r.spec != nil && r.spec.Format == spec.Protobuf
}

func (r *renderer) alternatives(lead string, list []*spec.Schema, sep string) []docx.Inline {
	parts := make([][]docx.Inline, len(list))
	for i, a := range list {
		parts[i] = r.typeOf(a)
	}
	return append([]docx.Inline{{Text: lead}}, join(parts, sep)...)
}

// plainType is typeOf without links, for places that hold only text.
func plainType(s *spec.Schema) string {
	if s == nil {
		return ""
	}
	if s.Name != "" {
		return s.Name
	}
	r := &renderer{models: map[*spec.Schema]string{}}
	var b strings.Builder
	for _, in := range r.typeOfInner(s) {
		b.WriteString(in.Text)
	}
	return b.String()
}

// notes is details for a value in a table row. A model's constraints and
// example belong to its own section, which the type column links to.
func (r *renderer) notes(s *spec.Schema) []string {
	if r.models[s] != "" {
		return nil
	}
	return details(s)
}

// details lists the constraints on a value as short sentences.
func details(s *spec.Schema) []string {
	if s == nil {
		return nil
	}
	var out []string
	if s.Deprecated {
		out = append(out, "Deprecated.")
	}
	if s.ReadOnly {
		out = append(out, "Read-only: returned by the API, ignored in requests.")
	}
	if s.WriteOnly {
		out = append(out, "Write-only: accepted in requests, never returned.")
	}
	if s.Closed {
		out = append(out, "No properties other than the listed ones are allowed.")
	}
	out = append(out, s.Notes...)
	if s.Immutable {
		out = append(out, "Immutable: set when the resource is created and never changed after.")
	}
	if len(s.Enum) > 0 {
		vals := make([]string, 0, len(s.Enum))
		for _, e := range s.Enum {
			v, _ := inlineExample(e)
			vals = append(vals, v)
		}
		out = append(out, "Allowed values: "+strings.Join(vals, ", ")+".")
	}
	if s.Default != nil {
		if v, ok := inlineExample(s.Default); ok {
			out = append(out, "Default: "+v+".")
		}
	}
	if b := bounds(s.Minimum, s.ExclusiveMinimum, s.Maximum, s.ExclusiveMaximum); b != "" {
		out = append(out, b)
	}
	if s.MultipleOf != "" {
		out = append(out, "A multiple of "+s.MultipleOf+".")
	}
	if l := lengthRange(s.MinLength, s.MaxLength, "character"); l != "" {
		out = append(out, l)
	}
	if l := lengthRange(s.MinItems, s.MaxItems, "item"); l != "" {
		out = append(out, l)
	}
	if s.UniqueItems {
		out = append(out, "Items are unique.")
	}
	if s.Pattern != "" {
		out = append(out, "Matches the pattern "+s.Pattern+".")
	}
	if s.Not != nil {
		if t := plainType(s.Not); t != "" && t != "any" {
			out = append(out, "Must not be "+t+".")
		}
	}
	var exs []string
	for _, ex := range s.Examples {
		if v, ok := inlineExample(ex); ok {
			exs = append(exs, v)
		}
	}
	switch {
	case len(exs) == 1:
		out = append(out, "Example: "+exs[0]+".")
	case len(exs) > 1:
		out = append(out, "Examples: "+strings.Join(exs, ", ")+".")
	case s.Example != nil:
		if v, ok := inlineExample(s.Example); ok {
			out = append(out, "Example: "+v+".")
		}
	}
	if d := s.Discriminator; d != nil && d.Property != "" && s.Name == "" {
		out = append(out, "The "+d.Property+" property says which alternative a value is.")
	}
	// A model's section says these in paragraphs of their own.
	if x := xmlNote(s.XML); x != "" && s.Name == "" {
		out = append(out, x)
	}
	if ed := s.ExternalDocs; ed != nil && s.Name == "" {
		out = append(out, "See "+ed.URL+".")
	}
	for _, o := range s.Options {
		out = append(out, "Option "+o+".")
	}
	return out
}

func bounds(min, xmin, max, xmax string) string {
	var lo, hi string
	switch {
	case xmin != "":
		lo = "greater than " + xmin
	case min != "":
		lo = "at least " + min
	}
	switch {
	case xmax != "":
		hi = "less than " + xmax
	case max != "":
		hi = "at most " + max
	}
	switch {
	case lo != "" && hi != "":
		return upperFirst(lo) + " and " + hi + "."
	case lo != "":
		return upperFirst(lo) + "."
	case hi != "":
		return upperFirst(hi) + "."
	}
	return ""
}

func lengthRange(min, max, unit string) string {
	plural := func(n string) string {
		if n == "1" {
			return n + " " + unit
		}
		return n + " " + unit + "s"
	}
	switch {
	case min != "" && max != "" && min == max:
		return "Exactly " + plural(max) + "."
	case min != "" && max != "":
		return "Between " + min + " and " + plural(max) + "."
	case min != "" && min != "0":
		return "At least " + plural(min) + "."
	case max != "":
		return "At most " + plural(max) + "."
	}
	return ""
}

// cell is a table cell holding a markdown description followed by notes.
// Tables inside a description cannot nest in a cell, so their rows are
// flattened into lines of text.
func (r *renderer) cell(desc string, notes []string) docx.Cell {
	var c docx.Cell
	for _, b := range r.markdown(desc) {
		if b.para != nil {
			c = append(c, *b.para)
			continue
		}
		c = append(c, docx.Para{Inlines: []docx.Inline{{Text: strings.Join(b.table.Header, " | ")}}})
		for _, row := range b.table.Rows {
			var cells []string
			for _, cell := range row {
				var parts []string
				for _, p := range cell {
					parts = append(parts, plain(p.Inlines))
				}
				cells = append(cells, strings.Join(parts, " "))
			}
			c = append(c, docx.Para{Inlines: []docx.Inline{{Text: strings.Join(cells, " | ")}}})
		}
	}
	if len(notes) > 0 {
		c = append(c, docx.Para{Inlines: []docx.Inline{{Text: strings.Join(notes, " ")}}})
	}
	return c
}

func (r *renderer) write(blocks []block) {
	for _, b := range blocks {
		if b.para != nil {
			r.doc.Para(*b.para)
		} else {
			r.doc.Table(*b.table)
		}
	}
}

// link returns text linked to url, or plain text when url is not one a
// reader can follow.
func link(text, url string) []docx.Inline {
	if text == "" {
		text = url
	}
	if !followable(url) {
		return []docx.Inline{{Text: text}}
	}
	return []docx.Inline{{Text: text, URL: url}}
}

func join(parts [][]docx.Inline, sep string) []docx.Inline {
	var out []docx.Inline
	for i, p := range parts {
		if i > 0 {
			out = append(out, docx.Inline{Text: sep})
		}
		out = append(out, p...)
	}
	return out
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
