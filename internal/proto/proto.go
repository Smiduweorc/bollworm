// Package proto reads protobuf service definitions into the model the
// renderer walks. Everything goes through a FileDescriptorSet: .proto
// sources are compiled into one first, so a set from buf or protoc and the
// sources it was built from make the same document.
package proto

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/spec"
	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
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
	// Every error is collected rather than stopping at the first, so one
	// run lists everything wrong with the file.
	var errs []error
	c := protocompile.Compiler{
		Resolver:       protocompile.WithStandardImports(protocompile.CompositeResolver{main, &protocompile.SourceResolver{ImportPaths: importPaths}}),
		SourceInfoMode: protocompile.SourceInfoStandard,
		Reporter: reporter.NewReporter(func(err reporter.ErrorWithPos) error {
			errs = append(errs, err)
			return nil
		}, nil),
	}
	files, err := c.Compile(context.Background(), name)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
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
// file in the set imports, or with all set, every file in it but those of
// google.protobuf and google.api. A type from a file missing from the set is
// shown by name.
func Parse(data []byte, all bool) (*spec.Spec, error) {
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
		// google.protobuf's own files are the well-known types, documented
		// by protobuf; in a set missing the file that imported one, it
		// would otherwise look like a file to describe.
		describe := !imported[f.GetName()] && !strings.HasPrefix(f.GetName(), "google/protobuf/")
		if all {
			describe = !strings.HasPrefix(f.GetName(), "google/protobuf/") && !strings.HasPrefix(f.GetName(), "google/api/")
		}
		if describe {
			fd, err := reg.FindFileByPath(f.GetName())
			if err != nil {
				return nil, err
			}
			roots = append(roots, fd)
		}
	}
	if len(roots) == 0 {
		return nil, errors.New("the FileDescriptorSet holds only google.protobuf files, or with --include-imports only google.protobuf and google.api files")
	}
	b := &builder{schemas: map[protoreflect.FullName]*spec.Schema{}, types: dynamicpb.NewTypes(reg)}
	return b.build(roots), nil
}

type builder struct {
	// pkg is the package of every described file, or empty if they differ.
	pkg     protoreflect.FullName
	schemas map[protoreflect.FullName]*spec.Schema
	models  []*spec.Schema
	// types resolves the custom options the set's own files declare.
	types    *dynamicpb.Types
	warnings []string
}

var versionSuffix = regexp.MustCompile(`^v\d+((alpha|beta)\d*)?$`)

func (b *builder) build(roots []protoreflect.FileDescriptor) *spec.Spec {
	b.pkg = roots[0].Package()
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
	s.Warnings = b.warnings
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
		Options:     b.options(md),
	}
	if l := opts.GetIdempotencyLevel(); l != descriptorpb.MethodOptions_IDEMPOTENCY_UNKNOWN {
		op.Options = append([]string{"idempotency_level: " + l.String()}, op.Options...)
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
	s := &spec.Schema{Name: b.name(md.FullName()), Description: describe(md), Deprecated: opts.GetDeprecated(), Options: b.options(md)}
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
		opts, _ := fd.Options().(*descriptorpb.FieldOptions)
		if slices.Contains(fieldBehavior(opts), annotations.FieldBehavior_REQUIRED) {
			p.Required = true
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
	s := &spec.Schema{Name: b.name(ed.FullName()), Description: describe(ed), Deprecated: opts.GetDeprecated(), Types: []string{"enum"}, Options: b.options(ed)}
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
			Options:     b.options(vd),
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
	own := spec.Schema{Description: describe(fd), Deprecated: opts.GetDeprecated(), Options: b.options(fd)}
	for _, fb := range fieldBehavior(opts) {
		switch fb {
		case annotations.FieldBehavior_OUTPUT_ONLY:
			own.ReadOnly = true
		case annotations.FieldBehavior_INPUT_ONLY:
			own.WriteOnly = true
		case annotations.FieldBehavior_IMMUTABLE:
			own.Immutable = true
		case annotations.FieldBehavior_OPTIONAL:
			own.Notes = append(own.Notes, "Optional: may be left unset.")
		case annotations.FieldBehavior_IDENTIFIER:
			own.Notes = append(own.Notes, "Identifier: the resource's name, set by the server on create and used to address it after.")
		case annotations.FieldBehavior_UNORDERED_LIST:
			own.Notes = append(own.Notes, "Unordered: the server may return the items in any order.")
		case annotations.FieldBehavior_NON_EMPTY_DEFAULT:
			own.Notes = append(own.Notes, "Never empty: when left unset, the server fills in a default.")
		}
	}
	if own.Description == "" && !own.Deprecated && !own.ReadOnly && !own.WriteOnly && !own.Immutable && len(own.Options) == 0 && len(own.Notes) == 0 {
		return s
	}
	// The model is shared, so the field's own words go on a wrapper, as
	// OpenAPI 3.0 puts a description beside a $ref.
	if s.Name != "" {
		own.AllOf = []*spec.Schema{s}
		return &own
	}
	s.Description, s.Deprecated, s.Options = own.Description, own.Deprecated, own.Options
	s.ReadOnly, s.WriteOnly, s.Immutable, s.Notes = own.ReadOnly, own.WriteOnly, own.Immutable, own.Notes
	return s
}

func fieldBehavior(opts *descriptorpb.FieldOptions) []annotations.FieldBehavior {
	if opts == nil || !proto.HasExtension(opts, annotations.E_FieldBehavior) {
		return nil
	}
	return proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
}

// printed are the extensions the manual already says in its own words.
var printed = map[protoreflect.FullName]bool{
	"google.api.http":           true,
	"google.api.field_behavior": true,
}

// options lists the custom options set on a descriptor, such as buf.validate
// rules, as "(name): value" lines in field-number order. The options are
// read again against the set's own files, so an option whose definition is
// in the set is understood even though bollworm was not built with it.
func (b *builder) options(d protoreflect.Descriptor) []string {
	m := d.Options()
	if m == nil || !m.ProtoReflect().IsValid() {
		return nil
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil || len(raw) == 0 {
		return nil
	}
	full := m.ProtoReflect().Type().New().Interface()
	if err := (proto.UnmarshalOptions{Resolver: resolver{b.types}}).Unmarshal(raw, full); err != nil {
		b.warn(d, "its options cannot be read (%v), so they are left out", err)
		return nil
	}
	for _, num := range unknownFields(full.ProtoReflect().GetUnknown()) {
		b.warn(d, "option number %d is not defined in any file bollworm was given, so it is left out; add the file that declares it", num)
	}
	var fields []protoreflect.FieldDescriptor
	values := map[protoreflect.FieldDescriptor]protoreflect.Value{}
	full.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.IsExtension() && !printed[fd.FullName()] {
			fields = append(fields, fd)
			values[fd] = v
		}
		return true
	})
	slices.SortFunc(fields, func(x, y protoreflect.FieldDescriptor) int { return cmp.Compare(x.Number(), y.Number()) })
	var out []string
	for _, fd := range fields {
		out = append(out, "("+string(fd.FullName())+"): "+formatValue(fd, values[fd]))
	}
	return out
}

// resolver finds an extension among the set's own files first, then among
// those bollworm was built with, such as google.api.http, which a set built
// without its imports may lack.
type resolver struct{ set *dynamicpb.Types }

func (r resolver) FindExtensionByName(name protoreflect.FullName) (protoreflect.ExtensionType, error) {
	if xt, err := r.set.FindExtensionByName(name); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByName(name)
}

func (r resolver) FindExtensionByNumber(msg protoreflect.FullName, num protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	if xt, err := r.set.FindExtensionByNumber(msg, num); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByNumber(msg, num)
}

func (b *builder) warn(d protoreflect.Descriptor, format string, args ...any) {
	w := string(d.FullName()) + ": " + fmt.Sprintf(format, args...)
	if loc := d.ParentFile().SourceLocations().ByDescriptor(d); loc.Path != nil {
		w = fmt.Sprintf("%s:%d:%d: %s", d.ParentFile().Path(), loc.StartLine+1, loc.StartColumn+1, w)
	}
	if !slices.Contains(b.warnings, w) {
		b.warnings = append(b.warnings, w)
	}
}

// unknownFields lists the field numbers in raw, each once.
func unknownFields(raw []byte) []protowire.Number {
	var out []protowire.Number
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			break
		}
		raw = raw[n:]
		m := protowire.ConsumeFieldValue(num, typ, raw)
		if m < 0 {
			break
		}
		raw = raw[m:]
		if !slices.Contains(out, num) {
			out = append(out, num)
		}
	}
	return out
}

// formatValue spells an option's value in protobuf text format, one line,
// with the same spacing every time; prototext deliberately varies its
// spacing between builds, which would make the output change for nothing.
func formatValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	switch {
	case fd.IsList():
		l := v.List()
		parts := make([]string, l.Len())
		for i := range l.Len() {
			parts[i] = formatSingle(fd, l.Get(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case fd.IsMap():
		var parts []string
		v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
			parts = append(parts, "{key: "+formatSingle(fd.MapKey(), k.Value())+", value: "+formatSingle(fd.MapValue(), mv)+"}")
			return true
		})
		slices.Sort(parts)
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return formatSingle(fd, v)
}

func formatSingle(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		var fields []protoreflect.FieldDescriptor
		vals := map[protoreflect.FieldDescriptor]protoreflect.Value{}
		v.Message().Range(func(f protoreflect.FieldDescriptor, fv protoreflect.Value) bool {
			fields = append(fields, f)
			vals[f] = fv
			return true
		})
		slices.SortFunc(fields, func(x, y protoreflect.FieldDescriptor) int { return cmp.Compare(x.Number(), y.Number()) })
		parts := make([]string, len(fields))
		for i, f := range fields {
			name := string(f.Name())
			if f.IsExtension() {
				name = "[" + string(f.FullName()) + "]"
			}
			parts[i] = name + ": " + formatValue(f, vals[f])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
			return string(ev.Name())
		}
		return strconv.Itoa(int(v.Enum()))
	case protoreflect.StringKind:
		return strconv.Quote(v.String())
	case protoreflect.BytesKind:
		return strconv.Quote(string(v.Bytes()))
	}
	return v.String()
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
// set's field tags are control characters no YAML or JSON document has, but
// a stray control byte in a text file is not enough: the bytes must also
// read as a set of named files.
func Detect(data []byte) (set, source bool) {
	for _, c := range data {
		if c < '\t' || c > '\r' && c < ' ' {
			return isSet(data), false
		}
	}
	return false, protoSource.Match(data)
}

func isSet(data []byte) bool {
	var set descriptorpb.FileDescriptorSet
	if proto.Unmarshal(data, &set) != nil || len(set.File) == 0 {
		return false
	}
	for _, f := range set.File {
		if f.GetName() == "" {
			return false
		}
	}
	return true
}

var protoSource = regexp.MustCompile(`^(?:\s+|//[^\n]*|/\*(?s:.*?)\*/)*(?:syntax|edition)\s*=`)
