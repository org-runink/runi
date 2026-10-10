// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SyntaxError says what was wrong and on which line of the input, counting
// from 1 and counting the fence if there was one, so the number matches what
// the caller is looking at.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("toon: line %d: %s", e.Line, e.Msg) }

// Decode parses text as TOON and stores the result in v, which must be a
// non-nil pointer. Field mapping follows encoding/json, including `json` tags,
// so a struct that already round-trips as JSON needs no new tags.
//
// A declared array length that disagrees with the number of values is taken as
// the model miscounting and the values win; use [Strict] to reject it.
func Decode(text string, v any) error { return decode(text, v, false) }

// Strict is Decode, except a declared array length that does not match the
// number of values is an error. Use it when the count is load-bearing — when
// it came from a schema the model was told to fill rather than from the model
// counting its own output.
func Strict(text string, v any) error { return decode(text, v, true) }

// marshalDoc re-marshals the parsed document so encoding/json can fill the
// caller's typed destination. It is a variable for the same reason
// jsonToValue is: the document was built by this package out of maps, slices
// and scalars, so there is nothing in it json.Marshal can refuse, and an error
// path that never runs is not known to work.
var marshalDoc = json.Marshal

func decode(text string, v any, strict bool) error {
	// A generic destination is filled straight from the parsed document. The
	// marshal-and-unmarshal below exists so encoding/json can map the document
	// onto a TYPED destination -- struct tags, custom unmarshalers, numeric
	// conversions -- but when the caller just wants the tree there is nothing
	// for it to map, and rendering the whole document to JSON only to parse it
	// straight back was most of Decode's cost.
	//
	// In that case the document is parsed with float64 numbers, so what the
	// caller sees is unchanged: integers arrive as float64, exactly as they
	// would through the round trip, including the 2^53 rounding Parse avoids.
	// Deciding this BEFORE parsing is the whole point -- converting afterwards
	// meant a second walk of the entire tree that re-boxed every number into a
	// fresh interface value and wrote back every key it passed, changed or not.
	generic := false
	switch v.(type) {
	case *any, *map[string]any, *[]any:
		generic = true
	}

	doc, err := parse(text, strict, generic)
	if err != nil {
		return err
	}

	switch dst := v.(type) {
	case *any:
		*dst = doc
		return nil
	case *map[string]any:
		if m, ok := doc.(map[string]any); ok {
			*dst = m
			return nil
		}
		// Shape mismatch: fall through so encoding/json produces its own
		// UnmarshalTypeError rather than this package inventing one.
	case *[]any:
		if a, ok := doc.([]any); ok {
			*dst = a
			return nil
		}
	}

	b, err := marshalDoc(doc)
	if err != nil {
		return fmt.Errorf("toon: %w", err)
	}
	return json.Unmarshal(b, v)
}

// Parse returns the document as map[string]any, []any and scalars, for callers
// that do not have a type to decode into.
func Parse(text string) (any, error) { return parse(text, false, false) }

const indentWidth = 2

type line struct {
	n      int // 1-based, in the original text
	indent int // levels, not spaces
	text   string
}

// parse is a single pass over the lines, holding a stack of open containers.
func parse(text string, strict, floatNums bool) (any, error) {
	lines, err := scan(text)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return map[string]any{}, nil
	}
	p := &parser{lines: lines, strict: strict, floatNums: floatNums}
	root, err := p.block(0)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.lines) {
		return nil, &SyntaxError{p.lines[p.i].n, "unexpected indentation"}
	}
	return root, nil
}

// scan strips a surrounding code fence, drops blank lines, and converts each
// remaining line's leading spaces into an indentation level.
func scan(text string) ([]line, error) {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start, end := 0, len(raw)
	for start < end && strings.TrimSpace(raw[start]) == "" {
		start++
	}
	// A fence is only a fence if it opens the content; a stray ``` inside is
	// a syntax error rather than something to strip.
	if start < end && strings.HasPrefix(strings.TrimSpace(raw[start]), "```") {
		start++
		for e := end - 1; e >= start; e-- {
			if strings.HasPrefix(strings.TrimSpace(raw[e]), "```") {
				end = e
				break
			}
		}
	}
	var out []line
	for i := start; i < end; i++ {
		s := raw[i]
		if strings.TrimSpace(s) == "" {
			continue
		}
		// Check the whole leading run of whitespace, not just the spaces: a
		// line indented with a tab has no leading spaces at all, so looking
		// only at those found nothing and let the tab through as depth 0.
		if lead := len(s) - len(strings.TrimLeft(s, " \t\v\f")); strings.ContainsAny(s[:lead], "\t\v\f") {
			return nil, &SyntaxError{i + 1, "tab in indentation; TOON indents with two spaces"}
		}
		spaces := len(s) - len(strings.TrimLeft(s, " "))
		if spaces%indentWidth != 0 {
			return nil, &SyntaxError{i + 1, fmt.Sprintf("indent of %d spaces is not a multiple of %d", spaces, indentWidth)}
		}
		out = append(out, line{n: i + 1, indent: spaces / indentWidth, text: strings.TrimRight(s[spaces:], " ")})
	}
	return out, nil
}

type parser struct {
	lines  []line
	i      int
	strict bool
	// floatNums makes scalar hand back float64 for whole numbers instead of
	// int64, which is what encoding/json would have produced. Set only for a
	// generic destination; Parse leaves it off and stays exact.
	floatNums bool
}

func (p *parser) peek() (line, bool) {
	if p.i < len(p.lines) {
		return p.lines[p.i], true
	}
	return line{}, false
}

// block reads every line at exactly depth, as either a mapping or a dash list.
func (p *parser) block(depth int) (any, error) {
	l, ok := p.peek()
	if !ok {
		return map[string]any{}, nil
	}
	if l.indent != depth {
		return nil, &SyntaxError{l.n, "unexpected indentation"}
	}
	if strings.HasPrefix(l.text, "- ") || l.text == "-" {
		return p.dashList(depth)
	}
	return p.mapping(depth)
}

func (p *parser) mapping(depth int) (any, error) {
	out := map[string]any{}
	for {
		l, ok := p.peek()
		if !ok || l.indent < depth {
			return out, nil
		}
		if l.indent > depth {
			return nil, &SyntaxError{l.n, "unexpected indentation"}
		}
		if strings.HasPrefix(l.text, "- ") || l.text == "-" {
			return nil, &SyntaxError{l.n, "list item where a key was expected"}
		}
		key, rest, err := splitKey(l)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key.name]; dup {
			return nil, &SyntaxError{l.n, fmt.Sprintf("duplicate key %q", key.name)}
		}
		p.i++
		val, err := p.value(key, rest, depth, l)
		if err != nil {
			return nil, err
		}
		out[key.name] = val
	}
}

func (p *parser) dashList(depth int) (any, error) {
	out := []any{}
	for {
		l, ok := p.peek()
		if !ok || l.indent < depth {
			return out, nil
		}
		if l.indent > depth {
			return nil, &SyntaxError{l.n, "unexpected indentation"}
		}
		if !strings.HasPrefix(l.text, "- ") && l.text != "-" {
			return out, nil
		}
		item := strings.TrimPrefix(strings.TrimPrefix(l.text, "-"), " ")
		p.i++
		if item == "" {
			v, err := p.block(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		// "- key: value" opens a mapping whose remaining keys are indented
		// one level in, which is how every TOON writer emits them.
		if k, rest, err := splitKey(line{n: l.n, indent: depth, text: item}); err == nil {
			m := map[string]any{}
			v, err := p.value(k, rest, depth+1, l)
			if err != nil {
				return nil, err
			}
			m[k.name] = v
			more, err := p.mappingInto(m, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, more)
			continue
		}
		v, err := scalar(item, l, p.floatNums)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

// mappingInto continues filling m with the keys indented at depth.
func (p *parser) mappingInto(m map[string]any, depth int) (map[string]any, error) {
	for {
		l, ok := p.peek()
		if !ok || l.indent != depth || strings.HasPrefix(l.text, "- ") {
			return m, nil
		}
		key, rest, err := splitKey(l)
		if err != nil {
			return nil, err
		}
		if _, dup := m[key.name]; dup {
			return nil, &SyntaxError{l.n, fmt.Sprintf("duplicate key %q", key.name)}
		}
		p.i++
		v, err := p.value(key, rest, depth, l)
		if err != nil {
			return nil, err
		}
		m[key.name] = v
	}
}
