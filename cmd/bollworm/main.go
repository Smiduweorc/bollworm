// Command bollworm turns an OpenAPI document into a Word reference manual.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Smiduweorc/bollworm/internal/docx"
	"github.com/Smiduweorc/bollworm/internal/render"
	"github.com/Smiduweorc/bollworm/internal/spec"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := rootCmd(stdin)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "bollworm: %v\n", err)
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
		Short:         "Turn an OpenAPI document into a Word reference manual",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(renderCmd(stdin))
	return root
}

func renderCmd(stdin io.Reader) *cobra.Command {
	var (
		output string
		force  bool
		remote bool
		paper  string
	)
	cmd := &cobra.Command{
		Use:   "render <spec> -o <file.docx>",
		Short: "Write a .docx manual for an OpenAPI 3.0, 3.1 or 3.2 document",
		Long: `Write a .docx manual for an OpenAPI 3.0, 3.1 or 3.2 document.

The spec is YAML or JSON, read from a file or from stdin when given as "-".
A $ref to another file is followed, relative to the file it is in (or to
the working directory for stdin). A $ref to an https URL is fetched only
with --allow-remote-refs; plain http is never fetched.

The manual has a title page, a table of contents, the introduction and
servers, authentication, one chapter per tag with a section per endpoint,
and a chapter of data models. Word fills in the contents' page numbers when
it opens the file and asks first.

An existing output file is never replaced unless --force is given.`,
		Example: `  bollworm render openapi.yaml -o api.docx
  curl -sSfL https://aur.archlinux.org/rpc/openapi.json | bollworm render - -o aur.docx
  bollworm render openapi.json -o - --paper letter > api.docx`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(*cobra.Command, []string) error {
			if paper != "a4" && paper != "letter" {
				return fmt.Errorf("invalid --paper %q (want a4 or letter)", paper)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			size := docx.A4
			if paper == "letter" {
				size = docx.Letter
			}
			j := job{input: args[0], output: output, force: force, paper: size, open: newOpener(remote)}
			if err := j.run(stdin, cmd.OutOrStdout()); err != nil {
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
	_ = cmd.MarkFlagRequired("output")
	_ = cmd.MarkFlagFilename("output", "docx")
	_ = cmd.RegisterFlagCompletionFunc("paper", cobra.FixedCompletions([]string{"a4", "letter"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// job is one render: where the spec comes from, where the document goes.
type job struct {
	input, output string
	force         bool
	paper         docx.Paper
	open          *opener
}

func (j job) run(stdin io.Reader, stdout io.Writer) error {
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
	s, err := spec.Parse(src, location, j.open.open)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	out, err := render.Render(s, j.paper)
	if err != nil {
		return err
	}
	if output == "-" {
		_, err := stdout.Write(out)
		return err
	}
	return writeFile(output, out, j.force)
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
