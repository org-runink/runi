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
// value, not a candidate of its own. Brackets inside JSON strings are ignored,
// and an opening bracket that is never closed yields nothing.
//
// A candidate is balanced, not necessarily valid JSON: Decode checks validity.
func Candidates(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		if end := closing(text, i); end > i {
			out = append(out, text[i:end+1])
			i = end
		}
	}
	return out
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

// closing returns the index of the bracket that closes the one at start,
// honouring string literals and escapes, or -1 if it is never closed.
func closing(s string, start int) int {
	open := s[start]
	shut := byte('}')
	if open == '[' {
		shut = ']'
	}
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
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
			inString = true
		case open:
			depth++
		case shut:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
