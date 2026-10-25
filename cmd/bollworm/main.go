// Command bollworm turns an OpenAPI or OpenRPC document, or a protobuf API,
// into a Word reference manual.
package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/proto"
	"github.com/Smiduweorc/bollworm/internal/render"
	"github.com/Smiduweorc/bollworm/internal/spec"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes. Anything cobra rejects before the command runs is a usage
// error; anything that goes wrong while converting is a failure.
const (
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// failure marks an error raised while converting, as opposed to one cobra
// raised while parsing the command line.
type failure struct{ error }

func (f failure) Unwrap() error { return f.error }

// usage is a command line that only turns out wrong once the input is read.
type usage struct{ error }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := rootCmd(stdin)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	// A document with several problems reports each on a line of its own.
	for _, line := range strings.Split(err.Error(), "\n") {
		fmt.Fprintf(stderr, "bollworm: %s\n", line)
	}
	var f failure
	if errors.As(err, &f) {
		return exitFailure
	}
	fmt.Fprintln(stderr, "Run 'bollworm render --help' for usage.")
	return exitUsage
}

func rootCmd(stdin io.Reader) *cobra.Command {
	root := &cobra.Command{
		Use:           "bollworm",
		Short:         "Turn an OpenAPI or OpenRPC document, or a protobuf API, into a Word reference manual",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(renderCmd(stdin))
	return root
}

func renderCmd(stdin io.Reader) *cobra.Command {
	var (
		output     string
		force      bool
		remote     bool
		paper      string
		protoPaths []string
		linkBase   string
		imports    bool
		strict     bool
		tocDepth   int
	)
	cmd := &cobra.Command{
		Use:   "render <spec> -o <file.docx>",
		Short: "Write a .docx manual for an OpenAPI 3.x or OpenRPC 1.x document or a protobuf API",
		Long: `Write a .docx manual for an OpenAPI 3.0, 3.1 or 3.2 document, an OpenRPC 1.x
document, or the gRPC services in a .proto file or a FileDescriptorSet.

The spec is read from a file or from stdin when given as "-". An OpenAPI
or OpenRPC spec is YAML or JSON. A $ref to another file is followed, relative to the
file it is in (or to the working directory for stdin). A $ref to an https
URL is fetched only with --allow-remote-refs; plain http is never fetched.

A .proto file is compiled with its imports, which are looked up in each
--proto-path in turn, or in the file's own directory when none is given.
google/protobuf/*.proto are built in. A FileDescriptorSet (.binpb, .pb,
.desc or .protoset, from buf build or protoc --descriptor_set_out) is read
as it is; build it with --include_source_info to keep the comments.

The manual has a title page, a table of contents, the introduction and
servers, authentication, one chapter per tag (or service) with a section
per endpoint (or method, or rpc), and a chapter of data models. Word fills in the
contents' page numbers when it opens the file and asks first.

Relative links in descriptions are resolved against --link-base. Without
it, a relative link is printed after its text, with a warning.

Anything in the spec the manual cannot show as written is reported on
stderr as a warning, and the manual is still written; with --strict a
warning fails the run instead. Every error found is reported, not only the
first.

An existing output file is never replaced unless --force is given.`,
		Example: `  bollworm render openapi.yaml -o api.docx
  curl -sSfL https://aur.archlinux.org/rpc/openapi.json | bollworm render - -o aur.docx
  bollworm render openapi.json -o - --paper letter > api.docx
  bollworm render openrpc.json -o api.docx
  bollworm render proto/library/v1/library.proto --proto-path proto -o library.docx
  buf build -o api.binpb && bollworm render api.binpb -o api.docx`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(*cobra.Command, []string) error {
			if paper != "a4" && paper != "letter" {
				return fmt.Errorf("invalid --paper %q (want a4 or letter)", paper)
			}
			if tocDepth < 1 || tocDepth > 3 {
				return fmt.Errorf("invalid --toc-depth %d (want 1, 2 or 3)", tocDepth)
			}
			if linkBase != "" {
				u, err := url.Parse(linkBase)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					return fmt.Errorf("invalid --link-base %q (want an absolute http or https URL)", linkBase)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			size := docx.A4
			if paper == "letter" {
				size = docx.Letter
			}
			j := job{input: args[0], output: output, force: force, open: newOpener(remote), protoPaths: protoPaths, imports: imports, strict: strict}
			j.render.Paper, j.render.TOCDepth = size, tocDepth
			if linkBase != "" {
				j.render.LinkBase, _ = url.Parse(linkBase)
			}
			err := j.run(stdin, cmd.OutOrStdout(), cmd.ErrOrStderr())
			var u usage
			if errors.As(err, &u) {
				return u.error
			}
			if err != nil {
				return failure{err}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&output, "output", "o", "", `file to write, or "-" for stdout (required)`)
	f.BoolVar(&force, "force", false, "replace the output file if it exists")
	f.BoolVar(&remote, "allow-remote-refs", false, "fetch $refs to https URLs (files are always read)")
	f.StringVar(&paper, "paper", "a4", "page size: a4 or letter")
	f.StringArrayVarP(&protoPaths, "proto-path", "I", nil, "directory to look for .proto imports in; repeatable, searched in order")
	f.BoolVar(&imports, "include-imports", false, "protobuf: also describe the services of imported files (not google.protobuf or google.api)")
	f.StringVar(&linkBase, "link-base", "", "absolute URL to resolve relative links in descriptions against")
	f.BoolVar(&strict, "strict", false, "fail, writing nothing, when there are warnings")
	f.IntVar(&tocDepth, "toc-depth", 2, "heading levels in the contents: 1 chapters, 2 endpoints, 3 their parts")
	_ = cmd.MarkFlagRequired("output")
	_ = cmd.MarkFlagFilename("output", "docx")
	_ = cmd.MarkFlagDirname("proto-path")
	_ = cmd.RegisterFlagCompletionFunc("paper", cobra.FixedCompletions([]string{"a4", "letter"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("toc-depth", cobra.FixedCompletions([]string{"1", "2", "3"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("link-base", cobra.NoFileCompletions)
	return cmd
}

// job is one render: where the spec comes from, where the document goes.
type job struct {
	input, output string
	force         bool
	render        render.Options
	open          *opener
	protoPaths    []string
	imports       bool
	strict        bool
}

func (j job) run(stdin io.Reader, stdout, stderr io.Writer) error {
	input, output := j.input, j.output
	if output == "-" {
		if f, ok := stdout.(*os.File); ok && isTerminal(f) {
			return errors.New("refusing to write a .docx to a terminal; redirect stdout or pass -o <file.docx>")
		}
	} else if input != "-" {
		if same, err := sameFile(input, output); err != nil {
			return err
		} else if same {
			return fmt.Errorf("%s is the input spec; choose another output file", output)
		}
	}

	var src []byte
	var err error
	name := input
	// A relative $ref resolves against the spec's own directory, or against
	// the working directory for a spec on stdin.
	var location string
	if input == "-" {
		name = "stdin"
		src, err = io.ReadAll(stdin)
		location = filepath.Join(".", "stdin")
	} else {
		src, err = os.ReadFile(input)
		location = input
	}
	if err != nil {
		return err
	}
	if location, err = filepath.Abs(location); err != nil {
		return err
	}

	set, source := proto.Detect(src)
	switch strings.ToLower(filepath.Ext(input)) {
	case ".proto":
		set, source = false, true
	case ".binpb", ".pb", ".desc", ".protoset":
		set, source = true, false
	case ".yaml", ".yml", ".json":
		set, source = false, false
	}
	if len(j.protoPaths) > 0 && !source {
		return usage{fmt.Errorf("--proto-path is for .proto input, and %s is not a .proto file", name)}
	}
	if j.imports && !source && !set {
		return usage{fmt.Errorf("--include-imports is for protobuf input, and %s is not a .proto file or a FileDescriptorSet", name)}
	}
	var s *spec.Spec
	switch {
	case source:
		s, err = j.compile(input, src)
	case set:
		s, err = proto.Parse(src, j.imports)
	default:
		s, err = spec.Parse(src, location, j.open.open)
	}
	// A compile error already starts with file:line:col.
	if err != nil && !source {
		return prefixed(name, err)
	}
	if err != nil {
		return err
	}
	out, rendered, err := render.Render(s, j.render)
	if err != nil {
		return err
	}
	// A protobuf warning already starts with the file it is in.
	for _, w := range s.Warnings {
		if source || set {
			fmt.Fprintf(stderr, "bollworm: warning: %s\n", w)
		} else {
			fmt.Fprintf(stderr, "bollworm: warning: %s: %s\n", name, w)
		}
	}
	for _, w := range rendered {
		fmt.Fprintf(stderr, "bollworm: warning: %s: %s\n", name, w)
	}
	if n := len(s.Warnings) + len(rendered); j.strict && n > 0 {
		noun := "warnings"
		if n == 1 {
			noun = "warning"
		}
		return fmt.Errorf("%s: %d %s, and --strict is set; nothing was written", name, n, noun)
	}
	if output == "-" {
		_, err := stdout.Write(out)
		return err
	}
	return writeFile(output, out, j.force)
}

// prefixed puts the input's name before each of err's refusals.
func prefixed(name string, err error) error {
	var errs spec.Errors
	if !errors.As(err, &errs) || len(errs) < 2 {
		return fmt.Errorf("%s: %w", name, err)
	}
	out := make([]error, len(errs))
	for i, e := range errs {
		out[i] = fmt.Errorf("%s: %w", name, e)
	}
	return errors.Join(out...)
}

// compile builds a .proto file the way protoc does: its name is its path
// under the import path that holds it, which is how other files import it
// and how errors name it.
func (j job) compile(input string, src []byte) (*spec.Spec, error) {
	paths := j.protoPaths
	var name string
	switch {
	case input == "-":
		name = "stdin"
		if len(paths) == 0 {
			paths = []string{"."}
		}
	case len(paths) == 0:
		name, paths = filepath.Base(input), []string{filepath.Dir(input)}
	default:
		abs, err := filepath.Abs(input)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			dir, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			if rel, err := filepath.Rel(dir, abs); err == nil && filepath.IsLocal(rel) {
				name = filepath.ToSlash(rel)
				break
			}
		}
		if name == "" {
			return nil, fmt.Errorf("%s is not under any --proto-path; add the directory its imports are relative to", input)
		}
	}
	set, err := proto.Compile(name, src, paths)
	if errors.Is(err, protoregistry.NotFound) {
		return nil, fmt.Errorf("%w (looked in %s; add the directory that holds it with --proto-path)", err, strings.Join(paths, ", "))
	}
	if err != nil {
		return nil, err
	}
	return proto.Parse(set, j.imports)
}

// isTerminal asks the terminal driver rather than checking for a character
// device, because /dev/null is one too and writing there is fine.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func sameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		// A missing input is reported when it is read.
		return false, nil
	}
	ib, err := os.Stat(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}

// writeFile writes data to path. A new file is created exclusively, so two
// runs racing for the same name cannot both win. Replacing an existing file
// goes through a temporary file and a rename, so a reader never sees half a
// document and a failed write leaves the old one in place.
func writeFile(path string, data []byte, force bool) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return createNew(path, data)
	case err != nil:
		return err
	case !force:
		return fmt.Errorf("%s already exists; pass --force to replace it", path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func createNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists; pass --force to replace it", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
