package render

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// exampleBudget caps one printed example, in bytes. YAML aliases let a short
// document describe an example of any size ("billion laughs"), and a manual
// gains nothing from the five-hundredth page of one payload.
const exampleBudget = 16 << 10

// truncated is printed in place of whatever an example had past the budget.
const truncated = "... (example cut short)"

// exampleJSON prints n as indented JSON, keeping the key order the author
// wrote, which encoding/json would sort away.
func exampleJSON(n *yaml.Node) string {
	p := &jsonPrinter{budget: exampleBudget}
	p.value(n, 0)
	if p.over {
		return strings.TrimRight(p.b.String(), " \n") + "\n" + truncated
	}
	return p.b.String()
}

// inlineExample prints n on one line when it is short enough to sit in a
// table cell, and reports false otherwise.
func inlineExample(n *yaml.Node) (string, bool) {
	n = deref(n)
	if n.Kind == yaml.ScalarNode {
		s := scalarJSON(n)
		return s, len(s) <= 60
	}
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(exampleJSON(n))); err != nil {
		return "", false
	}
	return b.String(), b.Len() <= 60
}

type jsonPrinter struct {
	b      strings.Builder
	budget int
	over   bool
}

func (p *jsonPrinter) write(s string) {
	if p.over {
		return
	}
	if p.b.Len()+len(s) > p.budget {
		p.over = true
		return
	}
	p.b.WriteString(s)
}

func (p *jsonPrinter) value(n *yaml.Node, depth int) {
	if p.over {
		return
	}
	n = deref(n)
	pad := strings.Repeat("  ", depth+1)
	switch n.Kind {
	case yaml.MappingNode:
		if len(n.Content) == 0 {
			p.write("{}")
			return
		}
		p.write("{\n")
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				p.write(",\n")
			}
			p.write(pad + quote(deref(n.Content[i]).Value) + ": ")
			p.value(n.Content[i+1], depth+1)
		}
		p.write("\n" + pad[2:] + "}")
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			p.write("[]")
			return
		}
		p.write("[\n")
		for i, c := range n.Content {
			if i > 0 {
				p.write(",\n")
			}
			p.write(pad)
			p.value(c, depth+1)
		}
		p.write("\n" + pad[2:] + "]")
	default:
		p.write(scalarJSON(n))
	}
}

// scalarJSON maps a YAML scalar to JSON. YAML spells numbers in ways JSON
// does not (0x1F, .inf), so anything that is not already a JSON number is
// printed as the string it was written as.
func scalarJSON(n *yaml.Node) string {
	switch n.ShortTag() {
	case "!!null":
		return "null"
	case "!!bool":
		return strings.ToLower(n.Value)
	case "!!int":
		if json.Valid([]byte(n.Value)) {
			return n.Value
		}
		if i, err := strconv.ParseInt(strings.ReplaceAll(n.Value, "_", ""), 0, 64); err == nil {
			return strconv.FormatInt(i, 10)
		}
	case "!!float":
		if json.Valid([]byte(n.Value)) {
			return n.Value
		}
		if f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
	}
	return quote(n.Value)
}

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}
