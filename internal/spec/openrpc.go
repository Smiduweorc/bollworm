package spec

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func (r *reader) readOpenRPC(v *yaml.Node) (*Spec, error) {
	if v.Value != "1" && !strings.HasPrefix(v.Value, "1.") {
		return nil, r.errAt(v, "OpenRPC %s is not supported; bollworm reads 1.x", v.Value)
	}
	s := &Spec{Format: OpenRPC, Version: v.Value}
	r.readInfo(s)
	methods := get(r.root, "methods")
	if methods == nil || methods.Kind != yaml.SequenceNode {
		return nil, r.errAt(methods, "methods must be a list of method objects")
	}
	var g grouper
	for _, mn := range items(methods) {
		op, tags, err := r.readMethod(mn, &g)
		if err != nil {
			r.fail(err)
			continue
		}
		g.file(tags, op)
	}
	s.Groups = g.done()
	r.readModels(s)
	return r.finish(s)
}

func (r *reader) readMethod(mn *yaml.Node, g *grouper) (*Operation, []string, error) {
	n, err := r.resolve(mn)
	if err != nil {
		return nil, nil, err
	}
	name := get(n, "name")
	if str(name) == "" {
		return nil, nil, r.errAt(n, "a method needs a name")
	}
	m := &Method{ParamStructure: str(get(n, "paramStructure"))}
	if m.ParamStructure == "" {
		m.ParamStructure = "either"
	}
	switch m.ParamStructure {
	case "by-name", "by-position", "either":
	default:
		return nil, nil, r.errAt(get(n, "paramStructure"), `paramStructure must be "by-name", "by-position" or "either", not %q`, m.ParamStructure)
	}
	op := &Operation{
		ID:           name.Value,
		Summary:      str(get(n, "summary")),
		Description:  str(get(n, "description")),
		Deprecated:   boolean(get(n, "deprecated")),
		JSONRPC:      m,
		ExternalDocs: externalDocs(get(n, "externalDocs")),
	}
	m.Servers = readServers(get(n, "servers"))
	for _, pn := range items(get(n, "params")) {
		p, err := r.contentDescriptor(pn)
		if err != nil {
			return nil, nil, err
		}
		m.Params = append(m.Params, p)
	}
	if rn := get(n, "result"); rn != nil {
		if m.Result, err = r.contentDescriptor(rn); err != nil {
			return nil, nil, err
		}
	}
	for _, en := range items(get(n, "errors")) {
		e, err := r.resolve(en)
		if err != nil {
			return nil, nil, err
		}
		m.Errors = append(m.Errors, RPCError{Code: str(get(e, "code")), Message: str(get(e, "message")), Data: get(e, "data")})
	}
	for _, xn := range items(get(n, "examples")) {
		x, err := r.resolve(xn)
		if err != nil {
			return nil, nil, err
		}
		p := Pairing{Name: str(get(x, "name")), Summary: str(get(x, "summary")), Description: str(get(x, "description"))}
		for _, vn := range items(get(x, "params")) {
			ex, err := r.resolve(vn)
			if err != nil {
				return nil, nil, err
			}
			p.Params = append(p.Params, ParamValue{Name: str(get(ex, "name")), Value: get(ex, "value"), External: str(get(ex, "externalValue"))})
		}
		if rn := get(x, "result"); rn != nil {
			ex, err := r.resolve(rn)
			if err != nil {
				return nil, nil, err
			}
			p.Result, p.ResultExternal = get(ex, "value"), str(get(ex, "externalValue"))
		}
		m.Examples = append(m.Examples, p)
	}
	var tags []string
	for _, tn := range items(get(n, "tags")) {
		t, err := r.resolve(tn)
		if err != nil {
			return nil, nil, err
		}
		// Tags have no top-level list, so a chapter's description is
		// whichever use of the tag first carries one.
		tag := str(get(t, "name"))
		if tag == "" || slices.Contains(tags, tag) {
			continue
		}
		tags = append(tags, tag)
		i := g.add(tag, str(get(t, "description")))
		if g.groups[i].ExternalDocs == nil {
			g.groups[i].ExternalDocs = externalDocs(get(t, "externalDocs"))
		}
	}
	return op, tags, nil
}

func (r *reader) contentDescriptor(n *yaml.Node) (*Parameter, error) {
	d, err := r.resolve(n)
	if err != nil {
		return nil, err
	}
	p := &Parameter{
		Name:        str(get(d, "name")),
		Description: str(get(d, "description")),
		Required:    boolean(get(d, "required")),
		Deprecated:  boolean(get(d, "deprecated")),
	}
	if p.Description == "" {
		p.Description = str(get(d, "summary"))
	}
	sn := get(d, "schema")
	if sn == nil {
		return nil, r.errAt(d, "content descriptor %q needs a schema", p.Name)
	}
	if p.Schema, err = r.schema(sn); err != nil {
		return nil, err
	}
	return p, nil
}
