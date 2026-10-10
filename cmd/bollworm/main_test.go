package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

func cli(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return result{code, out.String(), errOut.String()}
}

const aur = "../../testdata/aur.json"

func isDocx(t *testing.T, data []byte) {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			return
		}
	}
	t.Fatal("no word/document.xml")
}

func TestHelpExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"render", "--help"}, {}} {
		r := cli(t, "", args...)
		if r.code != 0 || !strings.Contains(r.stdout, "render") || r.stderr != "" {
			t.Errorf("%v: %+v", args, r)
		}
	}
	r := cli(t, "", "render", "--help")
	for _, want := range []string{"-o, --output", "--force", "--paper", "a4 or letter", "--allow-remote-refs", "bollworm render openapi.yaml -o api.docx"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("render help is missing %q", want)
		}
	}
}

func TestVersion(t *testing.T) {
	r := cli(t, "", "--version")
	if r.code != 0 || r.stdout != "bollworm version dev\n" {
		t.Errorf("%+v", r)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := map[string][]string{
		"no spec":         {"render", "-o", "x.docx"},
		"two specs":       {"render", "a", "b", "-o", "x.docx"},
		"no output":       {"render", aur},
		"unknown flag":    {"render", aur, "-o", "x.docx", "--fast"},
		"unknown command": {"draw", aur},
		"bad paper":       {"render", aur, "-o", "x.docx", "--paper", "a3"},
	}
	for name, args := range cases {
		r := cli(t, "", args...)
		if r.code != exitUsage || r.stdout != "" || !strings.HasPrefix(r.stderr, "bollworm: ") || !strings.Contains(r.stderr, "Run 'bollworm render --help' for usage.") {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := cli(t, "", "render", aur, "-o", "x.docx", "--paper", "a3"); !strings.Contains(r.stderr, `invalid --paper "a3" (want a4 or letter)`) {
		t.Errorf("paper message: %q", r.stderr)
	}
	if _, err := os.Stat("x.docx"); err == nil {
		os.Remove("x.docx")
		t.Error("a usage error wrote a file")
	}
}

func TestRenderWritesTheFileQuietly(t *testing.T) {
	out := filepath.Join(t.TempDir(), "aur.docx")
	r := cli(t, "", "render", aur, "-o", out)
	if r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("%+v", r)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	isDocx(t, data)
}

func TestPaperSizeReachesTheDocument(t *testing.T) {
	dir := t.TempDir()
	for paper, want := range map[string]string{"a4": `<w:pgSz w:w="11906" w:h="16838"/>`, "letter": `<w:pgSz w:w="12240" w:h="15840"/>`} {
		out := filepath.Join(dir, paper+".docx")
		if r := cli(t, "", "render", aur, "-o", out, "--paper", paper); r.code != 0 {
			t.Fatalf("%+v", r)
		}
		data, _ := os.ReadFile(out)
		z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		for _, f := range z.File {
			if f.Name == "word/document.xml" {
				rc, _ := f.Open()
				var b bytes.Buffer
				b.ReadFrom(rc)
				rc.Close()
				if !strings.Contains(b.String(), want) {
					t.Errorf("%s: page size missing", paper)
				}
			}
		}
	}
}

func TestExistingFileIsNotReplacedWithoutForce(t *testing.T) {
	out := filepath.Join(t.TempDir(), "keep.docx")
	if err := os.WriteFile(out, []byte("precious"), 0o640); err != nil {
		t.Fatal(err)
	}
	r := cli(t, "", "render", aur, "-o", out)
	if r.code != exitFailure || !strings.Contains(r.stderr, "keep.docx already exists; pass --force to replace it") {
		t.Errorf("%+v", r)
	}
	if b, _ := os.ReadFile(out); string(b) != "precious" {
		t.Error("the file was changed")
	}

	if r := cli(t, "", "render", aur, "-o", out, "--force"); r.code != 0 {
		t.Fatalf("--force: %+v", r)
	}
	data, _ := os.ReadFile(out)
	isDocx(t, data)
	info, _ := os.Stat(out)
	// Windows keeps no Unix permission bits to preserve.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("--force should keep mode 0640, got %v", info.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".keep.docx.*"))
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestForceRefusesToReplaceADirectory(t *testing.T) {
	dir := t.TempDir()
	r := cli(t, "", "render", aur, "-o", dir, "--force")
	if r.code != exitFailure || !strings.Contains(r.stderr, "is not a regular file") {
		t.Errorf("%+v", r)
	}
}

func TestOutputCannotBeTheInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "spec.json")
	src, _ := os.ReadFile(aur)
	os.WriteFile(in, src, 0o644)
	outs := []string{in}
	// Creating a symlink on Windows needs a privilege CI may not have.
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(in, link); err == nil {
		outs = append(outs, link)
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	for _, out := range outs {
		r := cli(t, "", "render", in, "-o", out, "--force")
		if r.code != exitFailure || !strings.Contains(r.stderr, "is the input spec; choose another output file") {
			t.Errorf("%s: %+v", out, r)
		}
	}
	if b, _ := os.ReadFile(in); !bytes.Equal(b, src) {
		t.Error("the spec was overwritten")
	}
}

func TestStdinAndStdout(t *testing.T) {
	src, _ := os.ReadFile(aur)
	r := cli(t, string(src), "render", "-", "-o", "-")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("%+v", r)
	}
	isDocx(t, []byte(r.stdout))

	out := filepath.Join(t.TempDir(), "file.docx")
	cli(t, "", "render", aur, "-o", out)
	file, _ := os.ReadFile(out)
	if !bytes.Equal(file, []byte(r.stdout)) {
		t.Error("stdin to stdout should give the same bytes as file to file")
	}
}

func TestStdoutToDevNullIsAllowed(t *testing.T) {
	// /dev/null is a character device; only a real terminal is refused.
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer null.Close()
	var errOut bytes.Buffer
	if code := run([]string{"render", aur, "-o", "-"}, strings.NewReader(""), null, &errOut); code != 0 {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}

// osNotFound returns the OS's own wording for a missing path, which differs
// by platform ("no such file or directory" vs "The system cannot find the file specified.").
func osNotFound(t *testing.T, path string) string {
	t.Helper()
	_, err := os.Stat(path)
	var pe *fs.PathError
	if !errors.As(err, &pe) {
		t.Fatalf("stat %s: want a PathError, got %v", path, err)
	}
	return pe.Err.Error()
}

func TestSpecProblemsExitOneWithTheReason(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {$ref: 'other.yaml#/A'}\n"), 0o644)
	missing := filepath.Join(dir, "missing.yaml")
	noDir := filepath.Join(dir, "no", "such", "dir.docx")
	cases := []struct {
		args []string
		in   string
		want string
	}{
		{[]string{"render", bad, "-o", filepath.Join(dir, "a.docx")}, "", "bad.yaml: line 4: $ref \"other.yaml#/A\": open "},
		{[]string{"render", "-", "-o", filepath.Join(dir, "b.docx")}, "swagger: '2.0'\n", "stdin: line 1: this is a Swagger 2.0 document"},
		{[]string{"render", missing, "-o", filepath.Join(dir, "c.docx")}, "", "missing.yaml: " + osNotFound(t, missing)},
		{[]string{"render", aur, "-o", noDir}, "", osNotFound(t, noDir)},
	}
	for _, c := range cases {
		r := cli(t, c.in, c.args...)
		if r.code != exitFailure || r.stdout != "" || !strings.HasPrefix(r.stderr, "bollworm: ") || !strings.Contains(r.stderr, c.want) || strings.Contains(r.stderr, "--help") {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
	for _, name := range []string{"a.docx", "b.docx", "c.docx"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was written for a spec that failed", name)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		r := cli(t, "", "completion", sh)
		if r.code != 0 || !strings.Contains(r.stdout, "bollworm") {
			t.Errorf("%s: exit %d, %d bytes", sh, r.code, len(r.stdout))
		}
	}
}
