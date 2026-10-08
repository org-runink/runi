// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// ErrNoJSON reports that no value in the text decoded into the destination.
var ErrNoJSON = errors.New("salvage: no JSON value in the text decoded into the destination")

// Candidates returns every top-level balanced JSON object or array in text, in
// order of appearance. A value nested inside another is part of its enclosing
// value, not a candidate of its own, and an opening bracket that is never
// closed yields nothing — not even the values inside it. See Scan if you need
// to know that the text was cut off.
//
// A candidate is balanced, not necessarily valid JSON: Decode checks validity.
func Candidates(text string) []string {
	vals, _ := Scan(text)
	if len(vals) == 0 {
		return nil
	}
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = v.JSON
	}
	return out
}

// Value is one balanced candidate and where it was found, so a caller can
// report a position or slice the surrounding text.
type Value struct {
	JSON  string // the candidate, text[Start:End]
	Start int    // byte offset of the opening bracket
	End   int    // byte offset just past the closing bracket
}

// Scan returns every top-level balanced value in text, and whether the text was
// truncated: a bracket was opened and never closed.
//
// Truncation is reported because it is the one failure that silently produces a
// WRONG answer rather than no answer. A reply cut off at a token limit leaves
// its outer object unclosed while the objects nested inside it are complete, so
// treating those as top-level hands back a fragment that decodes cleanly and
// means something entirely different from the whole. The case that matters: a
// reviewer asked for {"findings": [...]} is cut off mid-list, the first finding
// decodes on its own, its "findings" key is absent, and the caller reads zero
// findings from a reply that was reporting several. Scan yields nothing for the
// unclosed region and sets truncated, so a caller can fail closed.
//
// Scan is a single pass that honours JSON string literals and escapes and
// matches brackets by type, so "{"a": [1, 2]}" is one value and a "]" inside an
// object does not close it.
func Scan(text string) (vals []Value, truncated bool) {
	var stack []byte // the open brackets, outermost first
	start := -1
	inString, escaped := false, false

	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			// A string outside any bracket is not a candidate: Decode tries the
			// whole text for that. Inside one it is just content.
			inString = true
		case '{', '[':
			if len(stack) == 0 {
				start = i
			}
			stack = append(stack, c)
		case '}', ']':
			if len(stack) == 0 {
				// A stray closer. Not truncation — there was nothing to close.
				continue
			}
			want := byte('}')
			if stack[len(stack)-1] == '[' {
				want = ']'
			}
			if c != want {
				// Mismatched nesting, as in {"a": [}]}. The document is broken
				// beyond extraction; abandon the whole region rather than
				// guessing which bracket was meant.
				stack = stack[:0]
				start = -1
				truncated = true
				continue
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && start >= 0 {
				vals = append(vals, Value{JSON: text[start : i+1], Start: start, End: i + 1})
				start = -1
			}
		}
	}
	// Anything still open at the end was never closed. Its contents are NOT
	// emitted: see the note above on why that matters.
	if len(stack) > 0 || inString {
		truncated = true
	}
	return vals, truncated
}

// First returns the first candidate that is valid JSON, and false if there is
// none. It does not consider the text as a whole; use Decode for that.
func First(text string) (string, bool) {
	for _, c := range Candidates(text) {
		if json.Valid([]byte(c)) {
			return c, true
		}
	}
	return "", false
}

// Decode stores in v the first JSON value in text that decodes into it: the
// whole text if it is one JSON value, otherwise each of Candidates in order.
// Unknown object fields are ignored, as with json.Unmarshal. It returns
// ErrNoJSON if nothing decodes.
func Decode(text string, v any) error { return decode(text, v, false) }

// DecodeStrict is Decode, except a value carrying fields the destination does
// not have is not a match. Use it when the shape itself is the signal, so a
// different object in the same reply is not mistaken for the one you asked for.
func DecodeStrict(text string, v any) error { return decode(text, v, true) }

func decode(text string, v any, strict bool) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("salvage: Decode needs a non-nil pointer")
	}
	if trimmed := strings.TrimSpace(text); trimmed != "" && decodeOne(trimmed, v, strict) == nil {
		return nil
	}
	for _, c := range Candidates(text) {
		if decodeOne(c, v, strict) == nil {
			return nil
		}
	}
	return ErrNoJSON
}

// decodeOne decodes exactly one value from s into v, rejecting trailing data,
// so "{...} trailing prose" is a candidate's job, not the whole text's.
func decodeOne(s string, v any, strict bool) error {
	dec := json.NewDecoder(strings.NewReader(s))
	if strict {
		dec.DisallowUnknownFields()
	}
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("salvage: trailing data")
	}
	// Decode into a fresh value and assign only on success, so a candidate that
	// fails half-way never leaves v partly filled for the next attempt.
	dst := reflect.ValueOf(v).Elem()
	fresh := reflect.New(dst.Type())
	inner := json.NewDecoder(bytes.NewReader(raw))
	if strict {
		inner.DisallowUnknownFields()
	}
	if err := inner.Decode(fresh.Interface()); err != nil {
		return err
	}
	dst.Set(fresh.Elem())
	return nil
}
