// Package spec holds the model the renderer walks and reads OpenAPI 3.x into
// it, owning every decision about what a document means: which version is
// accepted, how $ref is resolved, how path-level parameters combine with an
// operation's own, and which order things appear in. The renderer never sees
// a $ref or a YAML node.
package spec

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is an API description reduced to what a reference manual prints.
type Spec struct {
	Format Format
	// Version is the OpenAPI version, or the protobuf syntax.
	Version     string
	Title       string
	APIVersion  string
	Summary     string
	Description string
	TermsURL    string
	Contact     Contact
	License     License
	Servers     []Server
	// Groups holds the operations by first tag: declared tags in order, then
	// tags first seen on an operation, then the untagged operations.
	Groups   []Group
	Webhooks []*Operation
	Schemes  []*SecurityScheme
	// Security is the document-wide requirement, nil when there is none.
	Security []Requirement
	// Models are the schemas under components/schemas, in document order,
	// or the messages and enums of a protobuf package.
	Models       []*Schema
	Files        []File
	ExternalDocs *ExternalDocs
	// Warnings are things the document says that the manual cannot show
	// as written, one sentence each, with the line when there is one.
	Warnings []string
}

// ExternalDocs points at documentation that lives outside the spec.
type ExternalDocs struct {
	URL, Description string
}

type Format int

const (
	OpenAPI Format = iota
	Protobuf
	OpenRPC
)

type File struct {
	Name, Package string
}

type Contact struct {
	Name, URL, Email string
}

type License struct {
	Name, URL string
}

type Server struct {
	URL, Description string
	Variables        []ServerVariable
}

type ServerVariable struct {
	Name, Default, Description string
	Enum                       []string
}

// Group is a tag and the operations filed under it. Name is empty for the
// operations that carry no tag.
type Group struct {
	Name         string
	Description  string
	ExternalDocs *ExternalDocs
	Operations   []*Operation
	// Also are operations that carry this tag too, but are described in
	// the chapter of their first tag.
	Also []*Operation
}

// Operation is one call, whatever the protocol. Exactly one of HTTP, RPC
// and JSONRPC is set.
type Operation struct {
	// ID is the operationId, or the rpc's or method's name.
	ID           string
	Summary      string
	Description  string
	Deprecated   bool
	ExternalDocs *ExternalDocs
	// Options are protobuf method options other than deprecated and
	// google.api.http, as "name: value" lines.
	Options []string
	// Input and Output are an rpc's messages; HTTP bodies are in Endpoint.
	Input, Output *Schema
	Streaming     Streaming
	HTTP          *Endpoint
	RPC           *RPC
	JSONRPC       *Method
}

type Streaming int

const (
	Unary Streaming = iota
	ServerStreaming
	ClientStreaming
	BidiStreaming
)

type Endpoint struct {
	Method      string
	Path        string
	Parameters  []*Parameter
	RequestBody *RequestBody
	Responses   []*Response
	// Security is the requirement that applies to this operation: its own
	// when it declares one, otherwise the document's. Nil means none applies.
	Security []Requirement
	// Callbacks are the requests the API may send back while or after
	// serving this one.
	Callbacks []Callback
}

// Callback is a named set of requests the API sends to a URL the caller
// gave it. Each operation's Path is the runtime expression for that URL.
type Callback struct {
	Name       string
	Operations []*Operation
}

type RPC struct {
	Service, Method string
	Bindings        []Binding
}

// Method is an OpenRPC method; a nil Result makes it a notification.
type Method struct {
	// ParamStructure is "by-name", "by-position" or "either".
	ParamStructure string
	Params         []*Parameter
	Result         *Parameter
	Errors         []RPCError
	Examples       []Pairing
	// Servers replace the document's servers for this method.
	Servers []Server
}

type RPCError struct {
	Code, Message string
	// Data is the error's data member, nil when it declares none.
	Data *yaml.Node
}

// Pairing is an example call; Result is nil when it has none.
type Pairing struct {
	Name, Summary, Description string
	Params                     []ParamValue
	Result                     *yaml.Node
	// ResultExternal is set instead of Result when the result's example
	// lives at a URL.
	ResultExternal string
}

type ParamValue struct {
	Name  string
	Value *yaml.Node
	// External is set instead of Value when the example lives at a URL.
	External string
}

// Binding is one google.api.http rule.
type Binding struct {
	Method, Path string
	// Body is a request field, "*" for the whole request, or empty for none.
	Body string
	// ResponseBody is a response field, or empty for the whole response.
	ResponseBody string
}

type Parameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Deprecated  bool
	Schema      *Schema
	Example     *yaml.Node
	// Style is how the value is serialized, empty when the spec leaves it
	// to the default for In. Explode is nil when the spec leaves it unset.
	Style   string
	Explode *bool
	// ContentType is set when the parameter is described by content rather
	// than schema.
	ContentType string
}

type RequestBody struct {
	Description string
	Required    bool
	Content     []*MediaType
}

type Response struct {
	Status      string
	Description string
	Headers     []*Header
	Content     []*MediaType
	Links       []*Link
}

// Link says how a value in this response feeds another operation.
type Link struct {
	Name        string
	Description string
	// OperationID or OperationRef names the target.
	OperationID, OperationRef string
	// Target is the operation the link names, nil when it names none in
	// this document.
	Target     *Operation
	Parameters []LinkParam
	// RequestBody is the expression or value for the target's body, nil
	// when the link sets none.
	RequestBody *yaml.Node
}

// LinkParam is a value for a parameter of the target: a runtime
// expression such as $response.body#/id, or a constant.
type LinkParam struct {
	Name  string
	Value *yaml.Node
}

type Header struct {
	Name        string
	Description string
	Required    bool
	Schema      *Schema
}

type MediaType struct {
	Type     string
	Schema   *Schema
	Examples []Example
	// Encoding describes how properties of a multipart or form body are
	// sent, in the spec's order.
	Encoding []Encoding
}

type Encoding struct {
	Property    string
	ContentType string
	Style       string
	Explode     *bool
	Headers     []*Header
}

type Example struct {
	Summary string
	Value   *yaml.Node
	// External is set instead of Value when the example lives at a URL.
	External string
}

// Requirement is one acceptable way to authenticate: every scheme in it
// applies at once. An empty Requirement means the call may be anonymous.
type Requirement []SchemeUse

type SchemeUse struct {
	Name   string
	Scopes []string
}

type SecurityScheme struct {
	Name        string
	Type        string
	Description string
	// In and ParamName are set for apiKey, HTTPScheme and BearerFormat for
	// http.
	In, ParamName            string
	HTTPScheme, BearerFormat string
	Flows                    []OAuthFlow
	OpenIDConnectURL         string
}

type OAuthFlow struct {
	Kind             string
	AuthorizationURL string
	TokenURL         string
	RefreshURL       string
	Scopes           []Scope
}

type Scope struct {
	Name, Description string
}

// Schema is a JSON Schema as far as a reader of the manual needs it. Schemas
// form a graph, not a tree: a component that refers to itself points back at
// the same *Schema.
type Schema struct {
	// Name is set on schemas defined under components/schemas.
	Name        string
	Title       string
	Description string
	// Types is empty when the schema does not restrict the type. A nullable
	// 3.0 schema gets "null" appended, so 3.0 and 3.1 read the same.
	Types      []string
	Format     string
	Enum       []*yaml.Node
	Const      *yaml.Node
	Default    *yaml.Node
	Example    *yaml.Node
	Deprecated bool
	ReadOnly   bool
	WriteOnly  bool
	Items      *Schema
	Properties []*Property
	// AdditionalProperties is the schema of a map's values, nil when the
	// schema says nothing about extra keys or forbids them.
	AdditionalProperties *Schema
	// Key is the key type of a protobuf map; OpenAPI keys are strings.
	Key                  *Schema
	AllOf, OneOf, AnyOf  []*Schema
	Not                  *Schema
	Minimum, Maximum     string
	ExclusiveMinimum     string
	ExclusiveMaximum     string
	MultipleOf           string
	MinLength, MaxLength string
	MinItems, MaxItems   string
	UniqueItems          bool
	Pattern              string
	// Values are a protobuf enum's values, with their numbers.
	Values []EnumValue
	// Discriminator names the property that tells oneOf alternatives apart.
	Discriminator *Discriminator
	XML           *XML
	ExternalDocs  *ExternalDocs
	// Immutable is protobuf's IMMUTABLE field behavior: set once, on create.
	Immutable bool
	// Never is the false schema, which no value matches.
	Never bool
	// Closed is additionalProperties (or unevaluatedProperties) false: no
	// keys but the listed properties.
	Closed bool
	// Examples are every example the schema gives; Example is the first.
	Examples []*yaml.Node
	// Notes are sentences about the value that have no field of their
	// own, such as protobuf field behaviors.
	Notes []string
	// Options are protobuf options other than deprecated and the field
	// behaviors above, as "name: value" lines.
	Options []string
}

type Discriminator struct {
	Property string
	Mapping  []DiscriminatorCase
}

// DiscriminatorCase maps a value of the discriminator property to the
// alternative it selects.
type DiscriminatorCase struct {
	Value string
	// Ref is the mapping as written; Schema is nil when it resolves to
	// nothing.
	Ref    string
	Schema *Schema
}

// XML is how a schema is spelled in an XML body; only what the spec sets is
// filled in.
type XML struct {
	Name, Namespace, Prefix string
	Attribute, Wrapped      bool
}

type EnumValue struct {
	Name        string
	Number      int32
	Description string
	Deprecated  bool
	Options     []string
}

type Property struct {
	Name     string
	Required bool
	Schema   *Schema
	Number   int32
	Oneof    string
}

// Error is a refusal to read the document. Line is the 1-based line where
// the problem sits, or 0 when no single line is at fault. Doc names the
// referenced document the line is in, and is empty for the spec itself.
type Error struct {
	Doc  string
	Line int
	Msg  string
	// reported marks a failure already described by an earlier Error.
	reported bool
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Doc != "" {
		b.WriteString(e.Doc + ": ")
	}
	if e.Line > 0 {
		fmt.Fprintf(&b, "line %d: ", e.Line)
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Errors are every refusal found in one document, in the order found.
type Errors []*Error

func (e Errors) Error() string {
	lines := make([]string, len(e))
	for i, x := range e {
		lines[i] = x.Error()
	}
	return strings.Join(lines, "\n")
}

// Unwrap lets errors.As find each refusal.
func (e Errors) Unwrap() []error {
	out := make([]error, len(e))
	for i, x := range e {
		out[i] = x
	}
	return out
}

// fail records a refusal and lets reading go on, so one run reports every
// problem instead of the first. The same refusal reached twice, through a
// component used in two places, is reported once.
func (r *reader) fail(err error) {
	var e *Error
	if !errors.As(err, &e) {
		e = &Error{Msg: err.Error()}
	}
	if e.reported || r.seenErr[e.Error()] {
		return
	}
	if r.seenErr == nil {
		r.seenErr = map[string]bool{}
	}
	r.seenErr[e.Error()] = true
	r.errs = append(r.errs, e)
}

// warn records something the manual cannot show as the document says it.
func (r *reader) warn(n *yaml.Node, format string, args ...any) {
	w := r.errAt(n, format, args...).Error()
	if !slices.Contains(r.warnings, w) {
		r.warnings = append(r.warnings, w)
	}
}

// errAt is a refusal pointing at n, in whichever document n came from.
func (r *reader) errAt(n *yaml.Node, format string, args ...any) *Error {
	e := &Error{Msg: fmt.Sprintf(format, args...)}
	if n != nil {
		e.Line = n.Line
		if d := r.owner[n]; d != nil && d != r.main {
			e.Doc = d.loc
		}
	}
	return e
}

// Methods in the order a path's operations are printed. query is the method
// OpenAPI 3.2 added; earlier documents never use the key.
var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}

// Parse reads an OpenAPI 3.0, 3.1 or 3.2 document in YAML or JSON.
// location is where src came from: an absolute file path or an https URL.
// A $ref to another document is resolved against it and handed to open,
// which returns that document's bytes or the reason it will not. With a nil
// open, any $ref outside src is refused.
func Parse(src []byte, location string, open Opener) (*Spec, error) {
	r := &reader{
		open:    open,
		docs:    map[string]*document{},
		owner:   map[*yaml.Node]*document{},
		schemas: map[*yaml.Node]*Schema{},
		names:   map[*yaml.Node]string{},
		taken:   map[string]*yaml.Node{},
		anchors: map[*document]map[string]*yaml.Node{},
		opNodes: map[*yaml.Node]*Operation{},
		ids:     map[string]*Operation{},
	}
	main, err := r.addDocument(location, src)
	if err != nil {
		return nil, err
	}
	r.main, r.root = main, main.root
	if r.root.Kind != yaml.MappingNode {
		return nil, r.errAt(r.root, "the document is not a mapping of OpenAPI fields")
	}
	for _, kv := range pairs(get(get(r.root, "components"), "schemas")) {
		r.names[kv.val] = kv.key
		r.taken[kv.key] = kv.val
	}
	return r.read()
}

type reader struct {
	main *document
	root *yaml.Node
	open Opener
	// docs holds every document read so far by location; owner maps each
	// node to its document, so a $ref resolves against the right one.
	docs  map[string]*document
	owner map[*yaml.Node]*document
	// schemas memoises every schema by the node it was built from, which is
	// what lets a recursive schema point back at itself.
	schemas map[*yaml.Node]*Schema
	// names maps each model's node to its name: the key under
	// components/schemas, or for a schema in another document, the key the
	// $ref named. taken is the reverse, to keep names unique.
	names map[*yaml.Node]string
	taken map[string]*yaml.Node
	// external lists models from other documents, first referenced first.
	external []*yaml.Node
	// anchors holds each document's $anchor and $dynamicAnchor names.
	anchors map[*document]map[string]*yaml.Node
	// opNodes maps each operation's node to it, for operationRef; ids maps
	// each operationId to the first operation with it.
	opNodes map[*yaml.Node]*Operation
	ids     map[string]*Operation
	// links are read before every operation is, so they are resolved last.
	links []pendingLink

	errs     Errors
	seenErr  map[string]bool
	badDocs  map[string]bool
	warnings []string
}

type pendingLink struct {
	link *Link
	node *yaml.Node
}

func (r *reader) read() (*Spec, error) {
	if sw := get(r.root, "swagger"); sw != nil {
		return nil, r.errAt(sw, "this is a Swagger %s document; bollworm reads OpenAPI 3.0 and later, so convert it first (for example with swagger2openapi)", sw.Value)
	}
	if v := get(r.root, "openrpc"); v != nil {
		return r.readOpenRPC(v)
	}
	v := get(r.root, "openapi")
	if v == nil {
		return nil, &Error{Msg: `no "openapi" or "openrpc" version field; this does not look like an OpenAPI or OpenRPC document`}
	}
	if !supported(v.Value) {
		return nil, r.errAt(v, "OpenAPI %s is not supported; bollworm reads 3.0, 3.1 and 3.2", v.Value)
	}
	s := &Spec{Version: v.Value}
	r.readInfo(s)
	var err error
	if s.Schemes, err = r.readSchemes(); err != nil {
		r.fail(err)
	}
	if sec := get(r.root, "security"); sec != nil {
		if s.Security, err = r.readSecurity(sec); err != nil {
			r.fail(err)
		}
	}
	s.Groups = r.readPaths(s.Security)
	// The document's security describes calls to the API. A webhook is a
	// call from it, so only a webhook's own requirement applies.
	s.Webhooks = r.readWebhooks()
	r.readModels(s)
	return r.finish(s)
}

// finish resolves what could only be resolved once every operation was
// read, and hands over the errors or the warnings.
func (r *reader) finish(s *Spec) (*Spec, error) {
	for _, pl := range r.links {
		l := pl.link
		switch {
		case l.OperationID != "":
			if l.Target = r.ids[l.OperationID]; l.Target == nil {
				r.warn(pl.node, "link %q names operationId %q, which no operation has", l.Name, l.OperationID)
			}
		case l.OperationRef != "":
			l.Target = r.operationAt(l.OperationRef, pl.node)
			if l.Target == nil {
				r.warn(pl.node, "link %q: operationRef %q does not lead to an operation in this document", l.Name, l.OperationRef)
			}
		}
	}
	if len(r.errs) > 0 {
		return nil, r.errs
	}
	s.Warnings = r.warnings
	return s, nil
}

func (r *reader) operationAt(ref string, at *yaml.Node) *Operation {
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ref, Line: at.Line}
	r.owner[val] = r.owner[at]
	n, err := r.pointer(val)
	if err != nil {
		return nil
	}
	return r.opNodes[n]
}

func (r *reader) readInfo(s *Spec) {
	info := get(r.root, "info")
	s.Title = str(get(info, "title"))
	s.APIVersion = str(get(info, "version"))
	s.Summary = str(get(info, "summary"))
	s.Description = str(get(info, "description"))
	s.TermsURL = str(get(info, "termsOfService"))
	c := get(info, "contact")
	s.Contact = Contact{Name: str(get(c, "name")), URL: str(get(c, "url")), Email: str(get(c, "email"))}
	l := get(info, "license")
	s.License = License{Name: str(get(l, "name")), URL: str(get(l, "url"))}
	if s.License.URL == "" {
		if id := str(get(l, "identifier")); id != "" {
			s.License.URL = "https://spdx.org/licenses/" + id + ".html"
		}
	}
	s.Servers = readServers(get(r.root, "servers"))
	s.ExternalDocs = externalDocs(get(r.root, "externalDocs"))
}

func externalDocs(n *yaml.Node) *ExternalDocs {
	u := str(get(n, "url"))
	if u == "" {
		return nil
	}
	return &ExternalDocs{URL: u, Description: str(get(n, "description"))}
}

func (r *reader) readModels(s *Spec) {
	seen := map[*Schema]bool{}
	for _, kv := range pairs(get(get(r.root, "components"), "schemas")) {
		m, err := r.schema(kv.val)
		if err != nil {
			r.fail(err)
			continue
		}
		// A component that is only a $ref to another is the same model.
		if !seen[m] {
			seen[m] = true
			s.Models = append(s.Models, m)
		}
	}
	for _, n := range r.external {
		if m := r.schemas[n]; !seen[m] {
			seen[m] = true
			s.Models = append(s.Models, m)
		}
	}
}

func supported(v string) bool {
	for _, p := range []string{"3.0", "3.1", "3.2"} {
		if v == p || strings.HasPrefix(v, p+".") {
			return true
		}
	}
	return false
}

func readServers(n *yaml.Node) []Server {
	var out []Server
	for _, sn := range items(n) {
		sv := Server{URL: str(get(sn, "url")), Description: str(get(sn, "description"))}
		for _, kv := range pairs(get(sn, "variables")) {
			v := ServerVariable{Name: kv.key, Default: str(get(kv.val, "default")), Description: str(get(kv.val, "description"))}
			for _, e := range items(get(kv.val, "enum")) {
				v.Enum = append(v.Enum, e.Value)
			}
			sv.Variables = append(sv.Variables, v)
		}
		out = append(out, sv)
	}
	return out
}

func (r *reader) readSchemes() ([]*SecurityScheme, error) {
	var out []*SecurityScheme
	for _, kv := range pairs(get(get(r.root, "components"), "securitySchemes")) {
		n, err := r.resolve(kv.val)
		if err != nil {
			return nil, err
		}
		sc := &SecurityScheme{
			Name:             kv.key,
			Type:             str(get(n, "type")),
			Description:      str(get(n, "description")),
			In:               str(get(n, "in")),
			ParamName:        str(get(n, "name")),
			HTTPScheme:       str(get(n, "scheme")),
			BearerFormat:     str(get(n, "bearerFormat")),
			OpenIDConnectURL: str(get(n, "openIdConnectUrl")),
		}
		for _, f := range pairs(get(n, "flows")) {
			flow := OAuthFlow{
				Kind:             f.key,
				AuthorizationURL: str(get(f.val, "authorizationUrl")),
				TokenURL:         str(get(f.val, "tokenUrl")),
				RefreshURL:       str(get(f.val, "refreshUrl")),
			}
			for _, sc := range pairs(get(f.val, "scopes")) {
				flow.Scopes = append(flow.Scopes, Scope{Name: sc.key, Description: str(sc.val)})
			}
			sc.Flows = append(sc.Flows, flow)
		}
		out = append(out, sc)
	}
	return out, nil
}

// readSecurity keeps an explicit empty list non-nil: "security: []" means no
// authentication, not the document's requirement.
func (r *reader) readSecurity(n *yaml.Node) ([]Requirement, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, r.errAt(n, "security must be a list of requirement objects")
	}
	out := []Requirement{}
	for _, rn := range items(n) {
		req := Requirement{}
		for _, kv := range pairs(rn) {
			use := SchemeUse{Name: kv.key}
			for _, sc := range items(kv.val) {
				use.Scopes = append(use.Scopes, sc.Value)
			}
			req = append(req, use)
		}
		out = append(out, req)
	}
	return out, nil
}

func (r *reader) readPaths(global []Requirement) []Group {
	var g grouper
	for _, t := range items(get(r.root, "tags")) {
		i := g.add(str(get(t, "name")), str(get(t, "description")))
		g.groups[i].ExternalDocs = externalDocs(get(t, "externalDocs"))
	}
	for _, kv := range pairs(get(r.root, "paths")) {
		ops, tags, err := r.readPathItem(kv.key, kv.val, global)
		if err != nil {
			r.fail(err)
		}
		for i, op := range ops {
			g.file(tags[i], op)
		}
	}
	return g.done()
}

// grouper files operations under their first tag, in first-seen order.
type grouper struct {
	groups   []Group
	index    map[string]int
	untagged []*Operation
}

func (g *grouper) add(name, desc string) int {
	if i, ok := g.index[name]; ok {
		if g.groups[i].Description == "" {
			g.groups[i].Description = desc
		}
		return i
	}
	if g.index == nil {
		g.index = map[string]int{}
	}
	g.index[name] = len(g.groups)
	g.groups = append(g.groups, Group{Name: name, Description: desc})
	return len(g.groups) - 1
}

// file puts op in the chapter of its first tag, and mentions it in the
// chapters of the others.
func (g *grouper) file(tags []string, op *Operation) {
	if len(tags) == 0 {
		g.untagged = append(g.untagged, op)
		return
	}
	i := g.add(tags[0], "")
	g.groups[i].Operations = append(g.groups[i].Operations, op)
	for _, t := range tags[1:] {
		if t == tags[0] {
			continue
		}
		j := g.add(t, "")
		if !slices.Contains(g.groups[j].Also, op) {
			g.groups[j].Also = append(g.groups[j].Also, op)
		}
	}
}

func (g *grouper) done() []Group {
	// A declared tag no operation uses would print an empty chapter.
	out := g.groups[:0]
	for _, gr := range g.groups {
		if len(gr.Operations) > 0 || len(gr.Also) > 0 {
			out = append(out, gr)
		}
	}
	if len(g.untagged) > 0 {
		out = append(out, Group{Operations: g.untagged})
	}
	return out
}

func (r *reader) readWebhooks() []*Operation {
	var out []*Operation
	for _, kv := range pairs(get(r.root, "webhooks")) {
		ops, _, err := r.readPathItem(kv.key, kv.val, nil)
		if err != nil {
			r.fail(err)
		}
		out = append(out, ops...)
	}
	return out
}

// readPathItem returns a path's operations and the tags of each. An
// operation that cannot be read is recorded as a refusal and left out, so
// the others are still checked.
func (r *reader) readPathItem(path string, n *yaml.Node, global []Requirement) ([]*Operation, [][]string, error) {
	item, err := r.resolve(n)
	if err != nil {
		return nil, nil, err
	}
	shared, err := r.readParameters(get(item, "parameters"))
	if err != nil {
		return nil, nil, err
	}
	var ops []*Operation
	var tags [][]string
	add := func(method string, on *yaml.Node) {
		op, tag, err := r.readOperation(method, path, on, shared, global)
		if err != nil {
			r.fail(err)
			return
		}
		ops = append(ops, op)
		tags = append(tags, tag)
	}
	for _, m := range methods {
		if on := get(item, m); on != nil {
			add(m, on)
		}
	}
	for _, kv := range pairs(get(item, "additionalOperations")) {
		add(kv.key, kv.val)
	}
	return ops, tags, nil
}

func (r *reader) readOperation(method, path string, n *yaml.Node, shared []*Parameter, global []Requirement) (*Operation, []string, error) {
	ep := &Endpoint{Method: strings.ToUpper(method), Path: path, Security: global}
	op := &Operation{
		ID:           str(get(n, "operationId")),
		Summary:      str(get(n, "summary")),
		Description:  str(get(n, "description")),
		Deprecated:   boolean(get(n, "deprecated")),
		HTTP:         ep,
		ExternalDocs: externalDocs(get(n, "externalDocs")),
	}
	r.noteOperation(op, n)
	own, err := r.readParameters(get(n, "parameters"))
	if err != nil {
		return nil, nil, err
	}
	ep.Parameters = mergeParameters(shared, own)

	if rb := get(n, "requestBody"); rb != nil {
		rn, err := r.resolve(rb)
		if err != nil {
			return nil, nil, err
		}
		body := &RequestBody{Description: str(get(rn, "description")), Required: boolean(get(rn, "required"))}
		if body.Content, err = r.readContent(get(rn, "content")); err != nil {
			return nil, nil, err
		}
		ep.RequestBody = body
	}

	for _, kv := range pairs(get(n, "responses")) {
		rn, err := r.resolve(kv.val)
		if err != nil {
			return nil, nil, err
		}
		resp := &Response{Status: kv.key, Description: str(get(rn, "description"))}
		if resp.Headers, err = r.readHeaders(get(rn, "headers")); err != nil {
			return nil, nil, err
		}
		if resp.Content, err = r.readContent(get(rn, "content")); err != nil {
			return nil, nil, err
		}
		for _, l := range pairs(get(rn, "links")) {
			ln, err := r.resolve(l.val)
			if err != nil {
				return nil, nil, err
			}
			link := &Link{
				Name:         l.key,
				Description:  str(get(ln, "description")),
				OperationID:  str(get(ln, "operationId")),
				OperationRef: str(get(ln, "operationRef")),
				RequestBody:  get(ln, "requestBody"),
			}
			for _, p := range pairs(get(ln, "parameters")) {
				link.Parameters = append(link.Parameters, LinkParam{Name: p.key, Value: p.val})
			}
			resp.Links = append(resp.Links, link)
			r.links = append(r.links, pendingLink{link, ln})
		}
		ep.Responses = append(ep.Responses, resp)
	}

	// A callback's requests come from the API, so the document's security,
	// which describes calls to it, does not apply to them.
	for _, cb := range pairs(get(n, "callbacks")) {
		cn, err := r.resolve(cb.val)
		if err != nil {
			return nil, nil, err
		}
		c := Callback{Name: cb.key}
		for _, kv := range pairs(cn) {
			ops, _, err := r.readPathItem(kv.key, kv.val, nil)
			if err != nil {
				return nil, nil, err
			}
			c.Operations = append(c.Operations, ops...)
		}
		if len(c.Operations) > 0 {
			ep.Callbacks = append(ep.Callbacks, c)
		}
	}

	if sec := get(n, "security"); sec != nil {
		if ep.Security, err = r.readSecurity(sec); err != nil {
			return nil, nil, err
		}
	}
	var tags []string
	for _, t := range items(get(n, "tags")) {
		if t.Value != "" && !slices.Contains(tags, t.Value) {
			tags = append(tags, t.Value)
		}
	}
	return op, tags, nil
}

// noteOperation records op for links to find. operationIds must be
// unique; when two share one, links and the manual's anchors use the first.
func (r *reader) noteOperation(op *Operation, n *yaml.Node) {
	r.opNodes[n] = op
	if op.ID == "" {
		return
	}
	if first, ok := r.ids[op.ID]; ok && first != op {
		r.warn(get(n, "operationId"), "operationId %q is used by more than one operation; links to it go to the first", op.ID)
		return
	}
	r.ids[op.ID] = op
}

func (r *reader) readHeaders(n *yaml.Node) ([]*Header, error) {
	var out []*Header
	for _, h := range pairs(n) {
		hn, err := r.resolve(h.val)
		if err != nil {
			return nil, err
		}
		hd := &Header{Name: h.key, Description: str(get(hn, "description")), Required: boolean(get(hn, "required"))}
		if hd.Schema, err = r.optionalSchema(get(hn, "schema")); err != nil {
			return nil, err
		}
		out = append(out, hd)
	}
	return out, nil
}

func optionalBool(n *yaml.Node) *bool {
	if n == nil || n.ShortTag() != "!!bool" {
		return nil
	}
	b := boolean(n)
	return &b
}

func (r *reader) readParameters(n *yaml.Node) ([]*Parameter, error) {
	var out []*Parameter
	for _, pn := range items(n) {
		p, err := r.resolve(pn)
		if err != nil {
			return nil, err
		}
		param := &Parameter{
			Name:        str(get(p, "name")),
			In:          str(get(p, "in")),
			Description: str(get(p, "description")),
			Required:    boolean(get(p, "required")),
			Deprecated:  boolean(get(p, "deprecated")),
			Example:     get(p, "example"),
			Style:       str(get(p, "style")),
			Explode:     optionalBool(get(p, "explode")),
		}
		if param.Schema, err = r.optionalSchema(get(p, "schema")); err != nil {
			return nil, err
		}
		// With content instead of schema, the schema is one level down.
		if param.Schema == nil {
			if c := pairs(get(p, "content")); len(c) > 1 {
				r.warn(get(p, "content"), "parameter %q has %d media types under content; a parameter has exactly one, and the first, %s, is used", param.Name, len(c), c[0].key)
			}
			for _, kv := range pairs(get(p, "content")) {
				param.ContentType = kv.key
				if param.Schema, err = r.optionalSchema(get(kv.val, "schema")); err != nil {
					return nil, err
				}
				break
			}
		}
		out = append(out, param)
	}
	return out, nil
}

// mergeParameters applies the rule from the OpenAPI spec: an operation's
// parameter replaces a path-level one with the same name and location.
func mergeParameters(shared, own []*Parameter) []*Parameter {
	if len(shared) == 0 {
		return own
	}
	out := make([]*Parameter, 0, len(shared)+len(own))
	for _, s := range shared {
		replaced := false
		for _, o := range own {
			if o.Name == s.Name && o.In == s.In {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, s)
		}
	}
	return append(out, own...)
}

func (r *reader) readContent(n *yaml.Node) ([]*MediaType, error) {
	var out []*MediaType
	for _, kv := range pairs(n) {
		mt := &MediaType{Type: kv.key}
		var err error
		if mt.Schema, err = r.optionalSchema(get(kv.val, "schema")); err != nil {
			return nil, err
		}
		if ex := get(kv.val, "example"); ex != nil {
			mt.Examples = append(mt.Examples, Example{Value: ex})
		}
		for _, e := range pairs(get(kv.val, "examples")) {
			en, err := r.resolve(e.val)
			if err != nil {
				return nil, err
			}
			ex := Example{Summary: str(get(en, "summary")), Value: get(en, "value"), External: str(get(en, "externalValue"))}
			if ex.Summary == "" {
				ex.Summary = e.key
			}
			mt.Examples = append(mt.Examples, ex)
		}
		for _, e := range pairs(get(kv.val, "encoding")) {
			enc := Encoding{
				Property:    e.key,
				ContentType: str(get(e.val, "contentType")),
				Style:       str(get(e.val, "style")),
				Explode:     optionalBool(get(e.val, "explode")),
			}
			if enc.Headers, err = r.readHeaders(get(e.val, "headers")); err != nil {
				return nil, err
			}
			mt.Encoding = append(mt.Encoding, enc)
		}
		out = append(out, mt)
	}
	return out, nil
}

func (r *reader) optionalSchema(n *yaml.Node) (*Schema, error) {
	if n == nil {
		return nil, nil
	}
	return r.schema(n)
}

// schema builds the Schema for n, following $ref. A schema still being built
// is returned half-finished, so recursion becomes a cycle of pointers.
func (r *reader) schema(n *yaml.Node) (*Schema, error) {
	n = deref(n)
	target := n
	var siblings *yaml.Node
	if ref := refOf(n); ref != nil {
		var last string
		var err error
		if target, last, err = r.follow(n); err != nil {
			return nil, err
		}
		r.nameModel(target, last, n)
		// 3.1 allows keywords beside $ref. They are layered over the
		// target below.
		if len(n.Content) > 2 {
			siblings = n
		}
	}
	s, ok := r.schemas[target]
	if !ok {
		s = &Schema{Name: r.names[target]}
		r.schemas[target] = s
		if err := r.fillSchema(s, target); err != nil {
			return nil, err
		}
	}
	if siblings == nil {
		return s, nil
	}
	// Wrapped, not copied: s may still be half-built inside a cycle.
	w := &Schema{AllOf: []*Schema{s}}
	if err := r.fillSchema(w, siblings); err != nil {
		return nil, err
	}
	return w, nil
}

// refOf is a schema's $ref, or its $dynamicRef. A $dynamicRef is resolved
// as if it were a $ref: the manual describes the schema as written, not as
// some other document extending it would see it.
func refOf(n *yaml.Node) *yaml.Node {
	if ref := get(n, "$ref"); ref != nil {
		return ref
	}
	return get(n, "$dynamicRef")
}

func (r *reader) fillSchema(s *Schema, n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" {
		// "true" accepts anything, which an empty schema says; "false"
		// accepts nothing.
		s.Never = !boolean(n)
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return r.errAt(n, "a schema must be a mapping")
	}
	s.Title = str(get(n, "title"))
	s.Description = str(get(n, "description"))
	s.Format = str(get(n, "format"))
	s.Deprecated = boolean(get(n, "deprecated"))
	s.ReadOnly = boolean(get(n, "readOnly"))
	s.WriteOnly = boolean(get(n, "writeOnly"))
	s.UniqueItems = boolean(get(n, "uniqueItems"))
	s.Pattern = str(get(n, "pattern"))
	s.Minimum = str(get(n, "minimum"))
	s.Maximum = str(get(n, "maximum"))
	s.MultipleOf = str(get(n, "multipleOf"))
	s.MinLength = str(get(n, "minLength"))
	s.MaxLength = str(get(n, "maxLength"))
	s.MinItems = str(get(n, "minItems"))
	s.MaxItems = str(get(n, "maxItems"))
	s.Const = get(n, "const")
	s.Default = get(n, "default")
	s.Example = get(n, "example")
	if s.Example != nil {
		s.Examples = []*yaml.Node{s.Example}
	}
	s.Examples = append(s.Examples, items(get(n, "examples"))...)
	if s.Example == nil && len(s.Examples) > 0 {
		s.Example = s.Examples[0]
	}
	s.Enum = items(get(n, "enum"))
	s.ExternalDocs = externalDocs(get(n, "externalDocs"))
	if x := get(n, "xml"); x != nil {
		s.XML = &XML{
			Name:      str(get(x, "name")),
			Namespace: str(get(x, "namespace")),
			Prefix:    str(get(x, "prefix")),
			Attribute: boolean(get(x, "attribute")) || str(get(x, "nodeType")) == "attribute",
			Wrapped:   boolean(get(x, "wrapped")),
		}
	}

	// 3.0 spells an exclusive bound as a boolean beside minimum; 3.1 makes
	// it the bound itself.
	if em := get(n, "exclusiveMinimum"); em != nil {
		if em.ShortTag() == "!!bool" {
			if boolean(em) {
				s.ExclusiveMinimum, s.Minimum = s.Minimum, ""
			}
		} else {
			s.ExclusiveMinimum = em.Value
		}
	}
	if em := get(n, "exclusiveMaximum"); em != nil {
		if em.ShortTag() == "!!bool" {
			if boolean(em) {
				s.ExclusiveMaximum, s.Maximum = s.Maximum, ""
			}
		} else {
			s.ExclusiveMaximum = em.Value
		}
	}

	if t := get(n, "type"); t != nil {
		if t.Kind == yaml.SequenceNode {
			for _, v := range items(t) {
				s.Types = append(s.Types, v.Value)
			}
		} else {
			s.Types = []string{t.Value}
		}
	}
	if boolean(get(n, "nullable")) && !slices.Contains(s.Types, "null") {
		s.Types = append(s.Types, "null")
	}

	var err error
	if s.Items, err = r.optionalSchema(get(n, "items")); err != nil {
		return err
	}
	if s.Not, err = r.optionalSchema(get(n, "not")); err != nil {
		return err
	}
	if ap := get(n, "additionalProperties"); ap != nil && ap.ShortTag() != "!!bool" {
		if s.AdditionalProperties, err = r.schema(ap); err != nil {
			return err
		}
	} else if boolean(ap) {
		s.AdditionalProperties = &Schema{}
	} else if ap != nil {
		s.Closed = true
	}
	if up := get(n, "unevaluatedProperties"); up != nil && up.ShortTag() == "!!bool" && !boolean(up) {
		s.Closed = true
	}
	for _, list := range []struct {
		key string
		dst *[]*Schema
	}{{"allOf", &s.AllOf}, {"oneOf", &s.OneOf}, {"anyOf", &s.AnyOf}} {
		for _, m := range items(get(n, list.key)) {
			ms, err := r.schema(m)
			if err != nil {
				return err
			}
			*list.dst = append(*list.dst, ms)
		}
	}

	required := map[string]bool{}
	for _, rq := range items(get(n, "required")) {
		required[rq.Value] = true
	}
	for _, kv := range pairs(get(n, "properties")) {
		ps, err := r.schema(kv.val)
		if err != nil {
			return err
		}
		s.Properties = append(s.Properties, &Property{Name: kv.key, Required: required[kv.key], Schema: ps})
	}
	if d := get(n, "discriminator"); d != nil {
		s.Discriminator = &Discriminator{Property: str(get(d, "propertyName"))}
		for _, kv := range pairs(get(d, "mapping")) {
			s.Discriminator.Mapping = append(s.Discriminator.Mapping, DiscriminatorCase{Value: kv.key, Ref: kv.val.Value, Schema: r.mappingTarget(kv.val, n)})
		}
	}
	return nil
}

// mappingTarget resolves a discriminator mapping value: a $ref-style
// reference, or the bare name of a schema under components/schemas. A value
// that resolves to nothing is kept as written rather than refusing a
// document over a mapping, which only a reader of the manual uses.
func (r *reader) mappingTarget(v, at *yaml.Node) *Schema {
	ref := v.Value
	if _, ok := r.taken[ref]; ok && !strings.Contains(ref, "#") {
		ref = "#/components/schemas/" + ref
	}
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ref, Line: v.Line}
	node := &yaml.Node{Kind: yaml.MappingNode, Line: v.Line, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "$ref"}, val}}
	// The synthetic $ref resolves against the document the mapping is in.
	r.owner[node], r.owner[val] = r.owner[at], r.owner[at]
	s, err := r.schema(node)
	if err != nil {
		msg := err.Error()
		var e *Error
		if errors.As(err, &e) {
			msg = e.Msg
		}
		r.warn(v, "discriminator mapping %q does not resolve, so it is shown as written: %s", v.Value, msg)
		return nil
	}
	return s
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func get(n *yaml.Node, key string) *yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return deref(n.Content[i+1])
		}
	}
	return nil
}

type pair struct {
	key string
	val *yaml.Node
}

// pairs returns a mapping's entries in the author's order, which the manual
// keeps.
func pairs(n *yaml.Node) []pair {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, pair{n.Content[i].Value, deref(n.Content[i+1])})
	}
	return out
}

func items(n *yaml.Node) []*yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		out[i] = deref(c)
	}
	return out
}

func str(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func boolean(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!bool" && strings.EqualFold(n.Value, "true")
}
