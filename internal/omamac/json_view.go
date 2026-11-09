package omamac

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
)

// ---------------------------------------------------------------------------
// A JSON parser that takes any prefix of a JSON text – the AI's answer while it
// is still being written – and returns as much of the value as has arrived.

type jKind byte

const (
	jObject jKind = 'o'
	jArray  jKind = 'a'
	jString jKind = 's'
	jScalar jKind = 'n' // number, true, false, null
)

// jNode is a parsed (possibly partial) JSON value.
type jNode struct {
	kind     jKind
	keys     []string // object
	vals     []*jNode // object values (same order as keys) or array items
	text     string   // string value or scalar literal
	complete bool
}

func (n *jNode) field(key string) *jNode {
	if n == nil || n.kind != jObject {
		return nil
	}
	for i, k := range n.keys {
		if k == key {
			return n.vals[i]
		}
	}
	return nil
}

// parsePartialJSON finds the first JSON value in s and parses as much of it as
// s holds. before and after are the text around it (prose, ``` fences).
func parsePartialJSON(s string) (node *jNode, before, after string) {
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil, s, ""
	}
	p := &partialParser{s: s, i: start}
	node = p.value()
	return node, s[:start], s[p.i:]
}

type partialParser struct {
	s string
	i int
}

func (p *partialParser) ws() {
	for p.i < len(p.s) && strings.IndexByte(" \t\r\n", p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *partialParser) value() *jNode {
	p.ws()
	if p.i >= len(p.s) {
		return nil
	}
	switch p.s[p.i] {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		return p.str()
	}
	start := p.i
	for p.i < len(p.s) && strings.IndexByte(",]} \t\r\n", p.s[p.i]) < 0 {
		p.i++
	}
	return &jNode{kind: jScalar, text: p.s[start:p.i], complete: p.i < len(p.s)}
}

func (p *partialParser) object() *jNode {
	n := &jNode{kind: jObject}
	p.i++ // {
	for {
		p.ws()
		if p.i >= len(p.s) {
			return n
		}
		switch p.s[p.i] {
		case '}':
			p.i++
			n.complete = true
			return n
		case ',':
			p.i++
			continue
		case '"':
		default:
			return n // not JSON any more: keep what we have
		}
		key := p.str()
		if !key.complete {
			return n
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return n
		}
		p.i++
		val := p.value()
		if val == nil {
			return n
		}
		n.keys = append(n.keys, key.text)
		n.vals = append(n.vals, val)
		if !val.complete {
			return n
		}
	}
}

func (p *partialParser) array() *jNode {
	n := &jNode{kind: jArray}
	p.i++ // [
	for {
		p.ws()
		if p.i >= len(p.s) {
			return n
		}
		switch p.s[p.i] {
		case ']':
			p.i++
			n.complete = true
			return n
		case ',':
			p.i++
			continue
		}
		val := p.value()
		if val == nil {
			return n
		}
		n.vals = append(n.vals, val)
		if !val.complete {
			return n
		}
	}
}

// str decodes a string, stopping where the text ends (an unfinished escape is dropped).
func (p *partialParser) str() *jNode {
	n := &jNode{kind: jString}
	p.i++ // "
	var b strings.Builder
	for p.i < len(p.s) {
		ch := p.s[p.i]
		switch {
		case ch == '"':
			p.i++
			n.text, n.complete = b.String(), true
			return n
		case ch != '\\':
			b.WriteByte(ch)
			p.i++
			continue
		}
		if p.i+1 >= len(p.s) {
			p.i = len(p.s)
			break
		}
		esc := p.s[p.i+1]
		p.i += 2
		switch esc {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
		case 'b', 'f':
		case 'u':
			r, ok := p.hexRune()
			if !ok {
				p.i = len(p.s)
				break
			}
			if utf16.IsSurrogate(r) && strings.HasPrefix(p.s[p.i:], `\u`) {
				p.i += 2
				if r2, ok := p.hexRune(); ok {
					r = utf16.DecodeRune(r, r2)
				}
			}
			b.WriteRune(r)
		default: // " \ /
			b.WriteByte(esc)
		}
	}
	n.text = b.String()
	return n
}

func (p *partialParser) hexRune() (rune, bool) {
	if p.i+4 > len(p.s) {
		return 0, false
	}
	v, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
	if err != nil {
		return 0, false
	}
	p.i += 4
	return rune(v), true
}

// compactJSON prints a node on one line (table cells holding nested values).
func compactJSON(n *jNode) string {
	if n == nil {
		return ""
	}
	switch n.kind {
	case jString:
		return n.text
	case jScalar:
		return n.text
	case jArray:
		parts := make([]string, len(n.vals))
		for i, v := range n.vals {
			parts[i] = compactJSON(v)
		}
		return strings.Join(parts, ", ")
	}
	parts := make([]string, len(n.keys))
	for i, k := range n.keys {
		parts[i] = k + ": " + compactJSON(n.vals[i])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// ---------------------------------------------------------------------------
// Rendering: fields as a table, lists of objects as tables, long text as code.

// renderJSONAnswer shows an AI answer (or a command's output): parsed into
// cards and tables, or – with raw – as indented JSON.
func (a *App) renderJSONAnswer(c *ui.Context, key, text string, raw bool) {
	node, before, after := parsePartialJSON(text)
	if raw || node == nil {
		codeBlock(c, formatJSONStream(text, true))
		return
	}
	prose := func(s string) {
		s = strings.TrimSpace(strings.NewReplacer("```json", "", "```", "").Replace(s))
		if s != "" {
			ui.Text(c, s).FontSize(12).FillWidth().Selectable()
		}
	}
	ui.Column(c.Key(key)).FillWidth().Gap(10).Children(func() {
		prose(before)
		if node.kind == jArray && allObjects(node.vals) && len(node.vals) > 0 && node.vals[0].field("title") != nil {
			for i, item := range node.vals { // a list of panels: one card each
				jsonCard(c, fmt.Sprintf("%s-card-%d", key, i), item)
			}
		} else {
			jsonCard(c, key+"-card", node)
		}
		prose(after)
	})
}

func allObjects(vals []*jNode) bool {
	for _, v := range vals {
		if v == nil || v.kind != jObject {
			return false
		}
	}
	return true
}

// jsonCard shows one value: an object as a field table with its lists and long texts below.
func jsonCard(c *ui.Context, key string, n *jNode) {
	t := c.Theme()
	ui.Column(c.Key(key)).FillWidth().Padding(10, 12).Radius(8).Border(1, t.Border).Background(t.Background).Gap(8).Children(func() {
		if n.kind != jObject {
			jsonValue(c, key+"-v", "", n)
			return
		}
		if title := n.field("title"); title != nil {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if icon := n.field("icon"); icon != nil {
					ui.Text(c, icon.text).FontSize(16)
				}
				ui.Text(c, title.text).FontSize(14).Bold().Grow(1).MinWidth(0)
			})
		}
		jsonFields(c, key, n, n.field("title") != nil)
	})
}

// jsonFields lays out an object: short values in a two-column table first,
// then lists, nested objects and long texts, each under its name.
func jsonFields(c *ui.Context, key string, n *jNode, inHeader bool) {
	t := c.Theme()
	var short, long []int
	for i, v := range n.vals {
		if inHeader && (n.keys[i] == "title" || n.keys[i] == "icon") {
			continue // already in the card's header
		}
		if isShortValue(v) {
			short = append(short, i)
		} else {
			long = append(long, i)
		}
	}
	if len(short) > 0 {
		ui.Column(c.Key(key+"-short")).FillWidth().Radius(6).Border(1, t.Border).Children(func() {
			for j, i := range short {
				if j > 0 {
					ui.Divider(c)
				}
				k, v := n.keys[i], n.vals[i]
				ui.Row(c.Key(fmt.Sprintf("%s-f-%d", key, i))).FillWidth().Padding(4, 10).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, k).Font("monospace").FontSize(11).TextColor(t.TextMuted).Width(140).SingleLine()
					jsonScalar(c, k, v)
				})
			}
		})
	}
	for _, i := range long {
		k, v := n.keys[i], n.vals[i]
		label := k
		if v.kind == jArray {
			label = fmt.Sprintf("%s (%d)", k, len(v.vals))
		}
		ui.Text(c, label).Font("monospace").FontSize(11).Bold().TextColor(t.TextMuted)
		jsonValue(c, fmt.Sprintf("%s-l-%d", key, i), k, v)
	}
}

func isShortValue(v *jNode) bool {
	switch v.kind {
	case jScalar:
		return true
	case jString:
		return !strings.Contains(v.text, "\n") && len(v.text) <= 120
	case jArray:
		for _, it := range v.vals {
			if it.kind == jObject || it.kind == jArray {
				return false
			}
		}
		return len(compactJSON(v)) <= 120
	}
	return false
}

// jsonScalar shows a short value; status-like values get their colored pill.
func jsonScalar(c *ui.Context, key string, v *jNode) {
	t := c.Theme()
	text := compactJSON(v)
	if key == "status" && v.kind == jString && v.text != "" {
		bg, fg := statusColors(t, v.text)
		pillBadge(c, v.text, bg, fg)
		return
	}
	col := t.Text
	if v.kind == jScalar {
		col = t.Accent
	}
	ui.Text(c, text).FontSize(12).TextColor(col).Grow(1).MinWidth(0).Selectable()
}

// jsonValue shows a long value: a list of objects as a table, other lists as
// bullet lines, a nested object as fields, a long string as code.
func jsonValue(c *ui.Context, key, name string, v *jNode) {
	t := c.Theme()
	switch v.kind {
	case jObject:
		ui.Column(c.Key(key)).FillWidth().Padding(0, 0, 0, 10).BorderWidth(0, 0, 0, 2).BorderColor(t.Border).Gap(6).Children(func() {
			jsonFields(c, key, v, false)
		})
	case jArray:
		if allObjects(v.vals) && len(v.vals) > 0 {
			jsonTable(c, key, v)
			return
		}
		ui.Column(c.Key(key)).FillWidth().Gap(2).Children(func() {
			for i, it := range v.vals {
				ui.Text(c.Key(fmt.Sprintf("%s-%d", key, i)), "• "+compactJSON(it)).FontSize(12).FillWidth().Selectable()
			}
		})
	case jString:
		codeBlock(c, v.text)
	default:
		jsonScalar(c, name, v)
	}
}

// jsonTable shows a list of objects with one column per key (in first-seen order).
func jsonTable(c *ui.Context, key string, v *jNode) {
	t := c.Theme()
	var cols []string
	seen := map[string]bool{}
	for _, row := range v.vals {
		for _, k := range row.keys {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	ui.Column(c.Key(key)).FillWidth().Radius(6).Border(1, t.Border).Clip().Children(func() {
		ui.Row(c).FillWidth().Padding(4, 10).Gap(10).Background(t.Surface).Children(func() {
			for _, col := range cols {
				ui.Text(c, col).Font("monospace").FontSize(11).Bold().TextColor(t.TextMuted).Grow(1).Basis(0).MinWidth(0).SingleLine()
			}
		})
		for r, row := range v.vals {
			ui.Divider(c)
			ui.Row(c.Key(fmt.Sprintf("%s-r-%d", key, r))).FillWidth().Padding(4, 10).Gap(10).AlignItems(ui.Center).Children(func() {
				for _, col := range cols {
					ui.Row(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
						cell := row.field(col)
						switch {
						case cell == nil:
							ui.Text(c, "")
						case col == "status" && cell.kind == jString && cell.text != "":
							bg, fg := statusColors(t, cell.text)
							pillBadge(c, cell.text, bg, fg)
						default:
							ui.Text(c, compactJSON(cell)).FontSize(11.5).MinWidth(0).SingleLine().Tooltip(compactJSON(cell))
						}
					})
				}
			})
		}
	})
}

func codeBlock(c *ui.Context, text string) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(8, 10).Radius(6).Border(1, t.Border).Background(t.Background).Children(func() {
		ui.Text(c, text).Font("monospace").FontSize(11).FillWidth().Selectable()
	})
}

// isJSONText reports whether s holds a JSON value at all (for choosing a view).
func isJSONText(s string) bool {
	n, _, _ := parsePartialJSON(s)
	return n != nil
}
