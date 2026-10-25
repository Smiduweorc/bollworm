package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// builtinStyle matches one style definition in the built-in styles part.
// Styles do not nest, so the first closing tag ends the definition.
var builtinStyle = regexp.MustCompile(`<w:style [^>]*w:styleId="([^"]+)".*?</w:style>`)

// Reference returns the styles part of the .docx in data, for Doc.Styles,
// with each style the renderer uses but the file lacks copied in from the
// built-in ones. missing names those styles, in the order they were added.
func Reference(data []byte) (out string, missing []string, err error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, errors.New("not a .docx file")
	}
	f, err := z.Open("word/styles.xml")
	if err != nil {
		return "", nil, errors.New("not a .docx file: it has no word/styles.xml")
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return "", nil, err
	}

	have := map[string]bool{}
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("word/styles.xml: %w", err)
		}
		if el, ok := tok.(xml.StartElement); ok && el.Name.Local == "style" {
			for _, a := range el.Attr {
				if a.Name.Local == "styleId" {
					have[a.Value] = true
				}
			}
		}
	}

	src := string(b)
	end := strings.LastIndex(src, "</w:styles>")
	if end < 0 {
		return "", nil, errors.New("word/styles.xml has no w:styles element")
	}
	var add strings.Builder
	for _, m := range builtinStyle.FindAllStringSubmatch(styles, -1) {
		if !have[m[1]] {
			have[m[1]] = true
			missing = append(missing, m[1])
			add.WriteString(m[0])
		}
	}
	return src[:end] + add.String() + src[end:], missing, nil
}
