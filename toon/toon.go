// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ErrInvalid is wrapped by every error from a bad option: an Options field
// outside the values its doc names, such as a Delimiter other than ',', '\t'
// or '|'. Match it with errors.Is; the message text is not part of the API.
var ErrInvalid = errors.New("toon: invalid option")

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
// so a struct that already round-trips as JSON needs no new tags, and a value
// that does not fit its field is the same error json.Unmarshal would give.
//
// Ordinary destinations -- structs, maps with string keys, slices, pointers,
// scalars and `any` -- are filled straight from the parsed document; no JSON is
// produced. A destination with its own UnmarshalJSON or UnmarshalText, and the
// struct shapes whose rules belong to encoding/json (embedded fields, the
// ",string" option, Go arrays), are filled through encoding/json instead.
//
// A declared array length that disagrees with the number of values is taken as
// the model miscounting and the values win; use [DecodeStrict] to reject it.
func Decode(text string, v any) error { return decode(text, v, false) }

// DecodeStrict is Decode, except a declared array length that does not match the
// number of values is an error. Use it when the count is load-bearing — when
// it came from a schema the model was told to fill rather than from the model
// counting its own output.
func DecodeStrict(text string, v any) error { return decode(text, v, true) }

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

	if !generic {
		rv := reflect.ValueOf(v)
		// A dry run first: it writes nothing, and only when it finds the
		// whole document fits is the destination filled. Otherwise it is
		// handed to encoding/json untouched, so the error and anything left
		// half-filled are exactly json's own.
		if rv.Kind() == reflect.Pointer && !rv.IsNil() && fromTree(doc, rv.Elem(), true) == nil {
			_ = fromTree(doc, rv.Elem(), false) // the dry run has already checked it
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
	var root any
	if first := lines[0]; first.indent == 0 && strings.HasPrefix(first.text, "[") {
		// A keyless header, "[3]: a,b,c" or "[2]{id,name}:", is the spec's
		// root array. Models trained on the spec write it, so it is read;
		// Encode still writes a top-level list under the key "items".
		k, rest, err := parseKey(first)
		if err != nil {
			return nil, err
		}
		p.i++
		root, err = p.array(k, rest, 0, first)
		if err != nil {
			return nil, err
		}
	} else if root, err = p.block(0); err != nil {
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
	// Two passes. The first records how far each line is indented; the second
	// turns those distances into depths, once the document has been read as a
	// whole and its own indentation unit is known.
	//
	// A single pass cannot do this. It would have to assume the unit, and
	// assuming two spaces is how a document indented with FOUR was read at
	// twice its true depth -- silently, because four is a multiple of two, so
	// nothing was out of place to complain about. Every key then nested under
	// the wrong parent and the caller received an empty map with a nil error.
	// A model that indents with three spaces was refused outright, and one
	// that indented the whole block by two had every line a level too deep.
	//
	// Language models do not reliably emit two spaces, so all three shapes
	// arrive in practice and the first is the dangerous one.
	type scanned struct {
		n      int
		spaces int
		text   string
	}
	var got []scanned
	for i := start; i < end; i++ {
		s := raw[i]
		if strings.TrimSpace(s) == "" {
			continue
		}
		// Check the whole leading run of whitespace, not just the spaces: a
		// line indented with a tab has no leading spaces at all, so looking
		// only at those found nothing and let the tab through as depth 0.
		if lead := len(s) - len(strings.TrimLeft(s, " \t\v\f")); strings.ContainsAny(s[:lead], "\t\v\f") {
			return nil, &SyntaxError{i + 1, "tab in indentation; TOON indents with spaces, not tabs"}
		}
		spaces := len(s) - len(strings.TrimLeft(s, " "))
		got = append(got, scanned{n: i + 1, spaces: spaces, text: strings.TrimRight(s[spaces:], " ")})
	}

	// A block indented as a whole -- inside a fence a model has over-indented,
	// or pasted under a bullet -- carries a common offset on every line,
	// including the first. It describes the same tree as the unshifted text,
	// so the shallowest line sets the zero and the rest are measured from it.
	base := 0
	if len(got) > 0 {
		base = got[0].spaces
		for _, g := range got[1:] {
			if g.spaces < base {
				base = g.spaces
			}
		}
	}

	// The unit is the largest step that divides every indent in the document:
	// their greatest common divisor. Two spaces gives 2, four gives 4, three
	// gives 3, and a document that never indents has no unit to find and does
	// not need one. Taking the GCD rather than the smallest gap matters when a
	// document jumps two levels at once -- 0, 4, 8 is a unit of 4, not 8.
	unit := 0
	for _, g := range got {
		unit = gcd(unit, g.spaces-base)
	}
	if unit == 0 {
		unit = indentWidth
	}

	// No divisibility check here, deliberately: the unit IS the GCD of these
	// very distances, so every one of them divides by it exactly. A guard
	// would be unreachable, and this package holds a 100% coverage floor --
	// an unreachable branch would have to be either excluded or faked, and
	// both are worse than not writing it.
	out := make([]line, 0, len(got))
	for _, g := range got {
		out = append(out, line{n: g.n, indent: (g.spaces - base) / unit, text: g.text})
	}
	return out, nil
}

// gcd is the greatest common divisor, with gcd(0, n) == n so it can be folded
// over a list starting from zero.
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

type parser struct {
	lines  []line
	i      int
	strict bool
	// floatNums makes scalar hand back float64 for whole numbers instead of
	// int64, which is what encoding/json would have produced. Set only for a
	// generic destination; Parse leaves it off and stays exact.
	floatNums bool
	// cells is reused by every row and inline array, so splitting a line
	// costs no allocation once the widest row has been seen.
	cells []string
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
