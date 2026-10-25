package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// zipped returns a package holding the given parts.
func zipped(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReferenceKeepsItsStylesAndFillsTheGaps(t *testing.T) {
	house := `<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:rPr><w:color w:val="C00000"/></w:rPr></w:style>`
	ref := zipped(t, map[string]string{"word/styles.xml": xmlHeader + `<w:styles ` + nsW + `>` + house + `</w:styles>`})
	out, missing, err := Reference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, house) || strings.Count(out, `w:styleId="Heading1"`) != 1 {
		t.Error("the reference's Heading1 was not kept as the only one")
	}
	if len(missing) == 0 || missing[0] != "Normal" {
		t.Errorf("missing = %v", missing)
	}
	for _, id := range missing {
		if id == "Heading1" {
			t.Error("Heading1 reported missing")
		}
		if !strings.Contains(out, `w:styleId="`+id+`"`) {
			t.Errorf("%s was not filled in", id)
		}
	}

	d := New("T", A4)
	d.Styles = out
	data, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if unpack(t, data)["word/styles.xml"] != out {
		t.Error("Doc.Styles did not become the styles part")
	}
}

func TestReferenceRefusesWhatIsNotADocx(t *testing.T) {
	for name, data := range map[string][]byte{
		"not a zip":  []byte("hello"),
		"no styles":  zipped(t, map[string]string{"word/document.xml": "<x/>"}),
		"broken xml": zipped(t, map[string]string{"word/styles.xml": "<w:styles>"}),
		"wrong root": zipped(t, map[string]string{"word/styles.xml": "<x/>"}),
	} {
		if _, _, err := Reference(data); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
