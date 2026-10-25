// Package proto reads protobuf service definitions into the model the
// renderer walks. Everything goes through a FileDescriptorSet: .proto
// sources are compiled into one first, so a set from buf or protoc and the
// sources it was built from make the same document.
package proto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/spec"
	"github.com/bufbuild/protocompile"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Compile returns a FileDescriptorSet of the .proto file name, whose
// contents are src, and its imports from importPaths. google/protobuf is
// built in. A missing import is an error wrapping protoregistry.NotFound.
func Compile(name string, src []byte, importPaths []string) ([]byte, error) {
	main := protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
		if path == name {
			return protocompile.SearchResult{Source: bytes.NewReader(src)}, nil
		}
		return protocompile.SearchResult{}, protoregistry.NotFound
	})
	c := protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(protocompile.CompositeResolver{main, &protocompile.SourceResolver{ImportPaths: importPaths}}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := c.Compile(context.Background(), name)
	if err != nil {
		return nil, err
	}
	set := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}
	var add func(protoreflect.FileDescriptor)
	add = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		for i := range fd.Imports().Len() {
			add(fd.Imports().Get(i).FileDescriptor)
		}
		set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
	}
	add(files[0])
	return proto.Marshal(set)
}

// Parse reads a binary FileDescriptorSet. It describes the files no other
// file in the set imports. A type from a file missing from the set is shown
// by name.
func Parse(data []byte) (*spec.Spec, error) {
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("not a FileDescriptorSet: %w", err)
	}
	if len(set.File) == 0 {
		return nil, errors.New("the FileDescriptorSet holds no files")
	}
	reg, err := protodesc.FileOptions{AllowUnresolvable: true}.NewFiles(&set)
	if err != nil {
		return nil, err
	}
	imported := map[string]bool{}
	for _, f := range set.File {
		for _, dep := range f.Dependency {
			imported[dep] = true
		}
	}
	var roots []protoreflect.FileDescriptor
	for _, f := range set.File {
		if !imported[f.GetName()] {
			fd, err := reg.FindFileByPath(f.GetName())
			if err != nil {
				return nil, err
			}
			roots = append(roots, fd)
		}
	}
	return build(roots), nil
}

type builder struct {
	// pkg is the package of every described file, or empty if they differ.
	pkg     protoreflect.FullName
	schemas map[protoreflect.FullName]*spec.Schema
	models  []*spec.Schema
}

var versionSuffix = regexp.MustCompile(`^v\d+((alpha|beta)\d*)?$`)

func build(roots []protoreflect.FileDescriptor) *spec.Spec {
	b := &builder{pkg: roots[0].Package(), schemas: map[protoreflect.FullName]*spec.Schema{}}
	for _, fd := range roots[1:] {
		if fd.Package() != b.pkg {
			b.pkg = ""
		}
	}
	s := &spec.Spec{Format: spec.Protobuf, Version: roots[0].Syntax().String(), Title: string(b.pkg)}
	if v := b.pkg.Name(); b.pkg != "" && versionSuffix.MatchString(string(v)) {
		s.APIVersion = string(v)
	}
	var intro []string
	for _, fd := range roots {
		s.Files = append(s.Files, spec.File{Name: fd.Path(), Package: string(fd.Package())})
		// The package statement is field 2 of FileDescriptorProto.
		if c := comments(fd.SourceLocations().ByPath(protoreflect.SourcePath{2})); c != "" {
			intro = append(intro, c)
		}
	}
	s.Description = strings.Join(intro, "\n\n")

	// Declared before any field refers to one, so models keep declaration
	// order and imported types come after them.
	var declared []protoreflect.MessageDescriptor
	for _, fd := range roots {
		declared = b.declare(declared, fd.Messages(), fd.Enums())
	}
	for _, md := range declared {
		b.fill(md, b.schemas[md.FullName()])
	}
	for _, fd := range roots {
		for i := range fd.Services().Len() {
			sd := fd.Services().Get(i)
			g := spec.Group{Name: string(sd.Name()), Description: describe(sd)}
			for j := range sd.Methods().Len() {
				g.Operations = append(g.Operations, b.method(sd.Methods().Get(j)))
			}
			s.Groups = append(s.Groups, g)
		}
	}
	s.Models = b.models
	return s
}

func (b *builder) declare(out []protoreflect.MessageDescriptor, msgs protoreflect.MessageDescriptors, enums protoreflect.EnumDescriptors) []protoreflect.MessageDescriptor {
	for i := range msgs.Len() {
		md := msgs.Get(i)
		if md.IsMapEntry() {
			continue
		}
		b.shell(md)
		out = b.declare(append(out, md), md.Messages(), md.Enums())
	}
	for i := range enums.Len() {
		b.enum(enums.Get(i))
	}
	return out
}

func (b *builder) method(md protoreflect.MethodDescriptor) *spec.Operation {
	opts, _ := md.Options().(*descriptorpb.MethodOptions)
	op := &spec.Operation{
		ID:          string(md.Name()),
		Description: describe(md),
		Deprecated:  opts.GetDeprecated(),
		Input:       b.message(md.Input()),
		Output:      b.message(md.Output()),
		RPC:         &spec.RPC{Service: string(md.Parent().FullName()), Method: string(md.Name())},
	}
	switch {
	case md.IsStreamingClient() && md.IsStreamingServer():
		op.Streaming = spec.BidiStreaming
	case md.IsStreamingClient():
		op.Streaming = spec.ClientStreaming
	case md.IsStreamingServer():
		op.Streaming = spec.ServerStreaming
	}
	if rule := httpRule(opts); rule != nil {
		op.RPC.Bindings = append(op.RPC.Bindings, binding(rule))
		for _, extra := range rule.GetAdditionalBindings() {
			op.RPC.Bindings = append(op.RPC.Bindings, binding(extra))
		}
	}
	return op
}

func httpRule(opts *descriptorpb.MethodOptions) *annotations.HttpRule {
	if opts == nil || !proto.HasExtension(opts, annotations.E_Http) {
		return nil
	}
	return proto.GetExtension(opts, annotations.E_Http).(*annotations.HttpRule)
}

func binding(r *annotations.HttpRule) spec.Binding {
	b := spec.Binding{Body: r.GetBody(), ResponseBody: r.GetResponseBody()}
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		b.Method, b.Path = "GET", p.Get
	case *annotations.HttpRule_Put:
		b.Method, b.Path = "PUT", p.Put
	case *annotations.HttpRule_Post:
		b.Method, b.Path = "POST", p.Post
	case *annotations.HttpRule_Delete:
		b.Method, b.Path = "DELETE", p.Delete
	case *annotations.HttpRule_Patch:
		b.Method, b.Path = "PATCH", p.Patch
	case *annotations.HttpRule_Custom:
		b.Method, b.Path = strings.ToUpper(p.Custom.GetKind()), p.Custom.GetPath()
	}
	return b
}

// wellKnown types are documented once by protobuf, so they are not expanded.
func wellKnown(name protoreflect.FullName) bool {
	return strings.HasPrefix(string(name), "google.protobuf.")
}

func (b *builder) name(full protoreflect.FullName) string {
	if b.pkg != "" && strings.HasPrefix(string(full), string(b.pkg)+".") {
		return string(full[len(b.pkg)+1:])
	}
	return string(full)
}

func (b *builder) message(md protoreflect.MessageDescriptor) *spec.Schema {
	if md.IsPlaceholder() || wellKnown(md.FullName()) {
		return &spec.Schema{Types: []string{string(md.FullName())}}
	}
	if s, ok := b.schemas[md.FullName()]; ok {
		return s
	}
	s := b.shell(md)
	b.fill(md, s)
	return s
}

// shell registers a message's model before its fields are read, so a
// message that contains itself points back at the same schema.
func (b *builder) shell(md protoreflect.MessageDescriptor) *spec.Schema {
	opts, _ := md.Options().(*descriptorpb.MessageOptions)
	s := &spec.Schema{Name: b.name(md.FullName()), Description: describe(md), Deprecated: opts.GetDeprecated()}
	b.schemas[md.FullName()] = s
	b.models = append(b.models, s)
	return s
}

func (b *builder) fill(md protoreflect.MessageDescriptor, s *spec.Schema) {
	for i := range md.Fields().Len() {
		fd := md.Fields().Get(i)
		p := &spec.Property{
			Name:     string(fd.Name()),
			Number:   int32(fd.Number()),
			Required: fd.Cardinality() == protoreflect.Required,
			Schema:   b.field(fd),
		}
		if o := fd.ContainingOneof(); o != nil && !o.IsSynthetic() {
			p.Oneof = string(o.Name())
		}
		s.Properties = append(s.Properties, p)
	}
}

func (b *builder) enum(ed protoreflect.EnumDescriptor) *spec.Schema {
	if ed.IsPlaceholder() || wellKnown(ed.FullName()) {
		return &spec.Schema{Types: []string{string(ed.FullName())}}
	}
	if s, ok := b.schemas[ed.FullName()]; ok {
		return s
	}
	opts, _ := ed.Options().(*descriptorpb.EnumOptions)
	s := &spec.Schema{Name: b.name(ed.FullName()), Description: describe(ed), Deprecated: opts.GetDeprecated(), Types: []string{"enum"}}
	b.schemas[ed.FullName()] = s
	b.models = append(b.models, s)
	for i := range ed.Values().Len() {
		vd := ed.Values().Get(i)
		vopts, _ := vd.Options().(*descriptorpb.EnumValueOptions)
		s.Values = append(s.Values, spec.EnumValue{
			Name:        string(vd.Name()),
			Number:      int32(vd.Number()),
			Description: describe(vd),
			Deprecated:  vopts.GetDeprecated(),
		})
	}
	return s
}

func (b *builder) field(fd protoreflect.FieldDescriptor) *spec.Schema {
	var s *spec.Schema
	switch {
	case fd.IsMap():
		s = &spec.Schema{Types: []string{"object"}, Key: b.value(fd.MapKey()), AdditionalProperties: b.value(fd.MapValue())}
	case fd.IsList():
		s = &spec.Schema{Types: []string{"array"}, Items: b.value(fd)}
	default:
		s = b.value(fd)
	}
	opts, _ := fd.Options().(*descriptorpb.FieldOptions)
	desc, deprecated := describe(fd), opts.GetDeprecated()
	if desc == "" && !deprecated {
		return s
	}
	// The model is shared, so the field's own words go on a wrapper, as
	// OpenAPI 3.0 puts a description beside a $ref.
	if s.Name != "" {
		return &spec.Schema{AllOf: []*spec.Schema{s}, Description: desc, Deprecated: deprecated}
	}
	s.Description, s.Deprecated = desc, deprecated
	return s
}

func (b *builder) value(fd protoreflect.FieldDescriptor) *spec.Schema {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return b.message(fd.Message())
	case protoreflect.EnumKind:
		return b.enum(fd.Enum())
	}
	return &spec.Schema{Types: []string{fd.Kind().String()}}
}

func describe(d protoreflect.Descriptor) string {
	return comments(d.ParentFile().SourceLocations().ByDescriptor(d))
}

// comments joins leading and trailing comments. protoc keeps the space after
// "//", which would indent every CommonMark line by one.
func comments(loc protoreflect.SourceLocation) string {
	var parts []string
	for _, c := range []string{loc.LeadingComments, loc.TrailingComments} {
		lines := strings.Split(strings.TrimRight(c, "\n "), "\n")
		for i, l := range lines {
			lines[i] = strings.TrimPrefix(l, " ")
		}
		if t := strings.TrimSpace(strings.Join(lines, "\n")); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// Detect reports whether data is a FileDescriptorSet or .proto source. A
// set's field tags are control characters no YAML or JSON document has.
func Detect(data []byte) (set, source bool) {
	for _, c := range data {
		if c < '\t' || c > '\r' && c < ' ' {
			return true, false
		}
	}
	return false, protoSource.Match(data)
}

var protoSource = regexp.MustCompile(`^(?:\s+|//[^\n]*|/\*(?s:.*?)\*/)*(?:syntax|edition)\s*=`)
