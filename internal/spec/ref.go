package spec

import (
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Opener returns the bytes of the document at location, which is an
// absolute file path or an https URL, or an error saying why it will not.
// Where those bytes may come from is the caller's decision, not this
// package's.
type Opener func(location string) ([]byte, error)

// maxDocuments bounds how many documents one spec may pull in. Strava's,
// split one schema family per file, uses 23 (itself and 22 fetched); a spec
// naming hundreds is broken or hostile, and each one could be a fetch.
const maxDocuments = 256

// maxRefChain bounds a chain of $ref objects pointing at each other. A chain
// that long is a loop: real documents use one or two hops.
const maxRefChain = 32

type document struct {
	loc  string
	root *yaml.Node
}

func (r *reader) addDocument(loc string, src []byte) (*document, error) {
	var doc yaml.Node
	d := &document{loc: loc}
	fail := func(msg string) error {
		e := &Error{Msg: msg}
		if len(r.docs) > 0 {
			e.Doc = loc
		}
		return e
	}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fail(strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fail("the document is empty")
	}
	d.root = deref(doc.Content[0])
	r.docs[loc] = d
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if _, seen := r.owner[n]; seen {
			return
		}
		r.owner[n] = d
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	return d, nil
}

// resolve follows n's $ref, if it has one, to the node it names.
func (r *reader) resolve(n *yaml.Node) (*yaml.Node, error) {
	target, _, err := r.follow(n)
	return target, err
}

// follow is resolve that also reports the last $ref it followed, which is
// what names a model found in another document.
func (r *reader) follow(n *yaml.Node) (*yaml.Node, string, error) {
	n = deref(n)
	last := ""
	for hops := 0; ; hops++ {
		ref := get(n, "$ref")
		if ref == nil || ref.Kind != yaml.ScalarNode {
			return n, last, nil
		}
		if hops == maxRefChain {
			return nil, "", r.errAt(ref, "$ref %q is part of a loop of references", ref.Value)
		}
		next, err := r.pointer(ref)
		if err != nil {
			return nil, "", err
		}
		n, last = next, ref.Value
	}
}

// pointer finds the node a $ref names: a document, located relative to the
// one the $ref is in, and a JSON pointer inside it.
func (r *reader) pointer(ref *yaml.Node) (*yaml.Node, error) {
	v := ref.Value
	target, frag, _ := strings.Cut(v, "#")
	d := r.owner[ref]
	if target != "" {
		var err error
		if d, err = r.load(d, target, ref); err != nil {
			return nil, err
		}
	}
	frag, err := url.PathUnescape(frag)
	if err != nil {
		return nil, r.errAt(ref, "$ref %q is not a valid URI fragment", v)
	}
	if frag != "" && !strings.HasPrefix(frag, "/") {
		return nil, r.errAt(ref, "$ref %q uses a named anchor; only JSON pointers like #/components/schemas/Pet are supported", v)
	}
	n := d.root
	if frag == "" {
		return n, nil
	}
	for _, tok := range strings.Split(frag[1:], "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		var next *yaml.Node
		switch n.Kind {
		case yaml.MappingNode:
			next = get(n, tok)
		case yaml.SequenceNode:
			if i, err := strconv.Atoi(tok); err == nil && i >= 0 && i < len(n.Content) {
				next = deref(n.Content[i])
			}
		}
		if next == nil {
			return nil, r.errAt(ref, "$ref %q does not resolve: nothing at %q", v, tok)
		}
		n = next
	}
	return n, nil
}

// load returns the document a $ref in from names, reading it on first use.
func (r *reader) load(from *document, target string, ref *yaml.Node) (*document, error) {
	if r.open == nil {
		return nil, r.errAt(ref, "$ref %q points outside this file; bundle the document into one file first (for example with redocly bundle)", ref.Value)
	}
	loc, err := locate(from.loc, target)
	if err != nil {
		return nil, r.errAt(ref, "$ref %q: %v", ref.Value, err)
	}
	if d, ok := r.docs[loc]; ok {
		return d, nil
	}
	if len(r.docs) >= maxDocuments {
		return nil, r.errAt(ref, "$ref %q would be document number %d; more than %d is refused", ref.Value, len(r.docs)+1, maxDocuments)
	}
	src, err := r.open(loc)
	if err != nil {
		return nil, r.errAt(ref, "$ref %q: %v", ref.Value, err)
	}
	return r.addDocument(loc, src)
}

// locate resolves a $ref's document part against the location of the
// document it appears in. Files may point at files or https URLs; a fetched
// document may only point at other https URLs, because a spec from the
// network must not be able to read files on this machine into the manual.
func locate(base, target string) (string, error) {
	if isURL(base) {
		b, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		t, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		u := b.ResolveReference(t)
		if u.Scheme != "https" {
			return "", errNotFromNetwork(u.String())
		}
		u.Fragment = ""
		return u.String(), nil
	}
	low := strings.ToLower(target)
	switch {
	case strings.HasPrefix(low, "https://"):
		u, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		u.Fragment = ""
		return u.String(), nil
	case strings.HasPrefix(low, "http://"):
		return "", errPlainHTTP
	case strings.HasPrefix(low, "file://"):
		u, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.FromSlash(u.Path)), nil
	case strings.Contains(target, "://"):
		return "", errScheme
	}
	p, err := url.PathUnescape(target)
	if err != nil {
		return "", err
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(base), p)
	}
	return filepath.Clean(p), nil
}

func isURL(loc string) bool {
	return strings.HasPrefix(loc, "https://")
}

type refError string

func (e refError) Error() string { return string(e) }

const (
	errPlainHTTP = refError("plain http is not fetched; only https")
	errScheme    = refError("only file paths and https URLs are followed")
)

func errNotFromNetwork(to string) error {
	return refError("a document fetched over the network cannot refer to " + to)
}

// nameModel names a schema reached through a $ref from the node from, when
// the schema has no name yet and should be a model: from is a component
// that is only a $ref, whose key the schema then takes over, or the schema
// lives in another document. The name is the last token of the $ref's pointer, or the
// document's file name when the $ref names a whole document. A name already
// used by a different schema gets the file name added.
func (r *reader) nameModel(target *yaml.Node, last string, from *yaml.Node) {
	preferred := r.names[from]
	if r.names[target] != "" {
		return
	}
	if preferred != "" && r.taken[preferred] == from {
		r.taken[preferred] = target
	}
	external := r.owner[target] != r.main
	if !external && preferred == "" {
		return
	}
	file, frag, _ := strings.Cut(last, "#")
	base := path.Base(strings.ReplaceAll(file, "\\", "/"))
	name := preferred
	if name == "" {
		if i := strings.LastIndex(frag, "/"); i >= 0 && i < len(frag)-1 {
			name = strings.ReplaceAll(strings.ReplaceAll(frag[i+1:], "~1", "/"), "~0", "~")
		} else {
			name = strings.TrimSuffix(base, path.Ext(base))
		}
	}
	if other, ok := r.taken[name]; ok && other != target {
		name += " (" + base + ")"
	}
	candidate := name
	for i := 2; r.taken[candidate] != nil && r.taken[candidate] != target; i++ {
		candidate = name + " " + strconv.Itoa(i)
	}
	name = candidate
	r.names[target] = name
	r.taken[name] = target
	if external {
		r.external = append(r.external, target)
	}
}
