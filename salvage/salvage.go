// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	looseQuotes := 0 // quotes seen in the prose AROUND the values

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
			if len(stack) == 0 {
				// A quote in the prose around a value is not the start of a
				// JSON string and must not be entered as one. Treating it as
				// one lets a single stray quote -- an explanation that opens a
				// quotation and never closes it -- swallow the value that
				// follows, so Scan reports one candidate where there were two
				// and DecodeOne hands back an answer instead of refusing an
				// ambiguous reply. Count the quote instead; the parity is used
				// below, and only to report truncation.
				looseQuotes++
				continue
			}
			// Inside a value a quote opens a real JSON string, where a bracket
			// is content rather than structure.
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
	// emitted: see the note above on why that matters. An odd number of prose
	// quotes says the text stops inside a quotation, which is the same
	// evidence of a cut-off reply even when every value in it closed; escapes
	// are not honoured out here, because prose is not JSON and the parity only
	// ever makes the caller more careful.
	if len(stack) > 0 || looseQuotes%2 == 1 {
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

// ErrTruncated reports that the text was cut off: a bracket, or a string, was
// opened and never closed.
var ErrTruncated = errors.New("salvage: the text is truncated")

// ErrAmbiguous reports that more than one value in the text decoded into the
// destination, so which one was meant is a guess.
var ErrAmbiguous = errors.New("salvage: more than one value decodes into the destination")

// DecodeOne is Decode for callers who need the reply to be unambiguous, and it
// fails closed. It returns ErrTruncated if the text was cut off, ErrAmbiguous
// if more than one value decodes, and ErrNoJSON if none does.
//
// This is the one to reach for when the decoded value drives a decision rather
// than being shown to someone. A model asked for a verdict and cut off at a
// token limit, or answering twice, is not a verdict; Decode would hand back the
// first thing that fits, which is how a truncated review becomes an empty list
// of findings and reads as approval. Nine call sites that each check those two
// conditions by hand is the same mistake written nine times, so it is written
// once, here.
func DecodeOne(text string, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("salvage: DecodeOne needs a non-nil pointer")
	}
	vals, truncated := Scan(text)
	if truncated {
		return ErrTruncated
	}
	// The whole text, when it is itself one value, is the unambiguous case.
	if trimmed := strings.TrimSpace(text); trimmed != "" && decodeOne(trimmed, v, false) == nil {
		return nil
	}
	// Counting is done against a scratch value of the destination's type, not
	// against the destination itself. Decoding into v to find out whether a
	// candidate fits leaves the last one that fitted sitting in v, so a caller
	// that got ErrAmbiguous and did not zero its variable would read a value
	// this function explicitly refused to choose — the one failure mode
	// DecodeOne exists to prevent, reintroduced by the check for it.
	dst := reflect.ValueOf(v).Elem()
	scratch := reflect.New(dst.Type()).Interface()

	var matches []string
	for _, c := range vals {
		for _, cand := range unwrap(c.JSON) {
			if decodeOne(cand, scratch, false) == nil {
				matches = append(matches, cand)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return ErrNoJSON
	case 1:
		return decodeOne(matches[0], v, false)
	default:
		return fmt.Errorf("%w: %d of them", ErrAmbiguous, len(matches))
	}
}

// unwrap yields a candidate and, when it is an array holding exactly one
// object, that object too. Models asked for an object routinely return it
// wrapped in a list of one, and a caller who asked for an object should not
// have to own a second type to read it. More than one element is left alone:
// picking from a list is a choice, not an unwrapping.
func unwrap(c string) []string {
	out := []string{c}
	t := strings.TrimSpace(c)
	if !strings.HasPrefix(t, "[") {
		return out
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(t), &raw); err != nil || len(raw) != 1 {
		return out
	}
	return append(out, string(raw[0]))
}

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
	// json.Decoder.More reports whether another element follows INSIDE the
	// array or object being parsed, so at the top level it answers false for a
	// leading ']' or '}'. Using it as a trailing-data guard therefore accepts
	// `{"a":1}}` and `{"a":1}] [{"b":2}]` as a single value, which is how a
	// reply holding two answers reaches a caller that asked DecodeOne to
	// refuse exactly that. Measure against the input instead: everything after
	// the value must be blank.
	if rest := strings.TrimSpace(s[dec.InputOffset():]); rest != "" {
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
