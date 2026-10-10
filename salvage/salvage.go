// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
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
	truncated = scanValues(text, func(v Value) bool {
		vals = append(vals, v)
		return true
	})
	return vals, truncated
}

// scanValues is Scan, reporting each value to yield as it is found instead of
// building a slice, so a caller that wants one value does not pay for a slice
// and a caller that stops early does not pay for the rest of the text. yield
// returning false ends the walk; truncated is then only what was seen so far,
// which is why Scan, the one caller that promises it, never stops early.
//
// The walk skips what cannot matter. Outside every value — depth 0 — the only
// byte that changes anything is an opening bracket, so it jumps to the next
// one with IndexByte rather than looking at each byte of the prose, and inside
// a string literal it jumps to the closing quote the same way. Both are the
// hardware's memchr; the loop they replace was the package reading a model's
// preamble one byte at a time.
func scanValues(text string, yield func(Value) bool) (truncated bool) {
	// The open brackets, outermost first. Replies nest a handful deep, so the
	// stack lives in this frame and never reaches the heap.
	var inline [16]byte
	stack := inline[:0]

	start := -1
	looseQuotes := 0 // quotes seen in the prose AROUND the values

	// Where the next opening bracket of each kind is, so that a text with a
	// thousand objects and no array in it does not re-scan the tail for a "["
	// after every one of them. len(text) means "there is no other one".
	nextObj, nextArr := indexFrom(text, '{', 0), indexFrom(text, '[', 0)

	for i := 0; i < len(text); {
		if len(stack) == 0 {
			if nextObj < i {
				nextObj = indexFrom(text, '{', i)
			}
			if nextArr < i {
				nextArr = indexFrom(text, '[', i)
			}
			j := nextObj
			if nextArr < j {
				j = nextArr
			}
			// A quote in the prose around a value is not the start of a JSON
			// string and must not be entered as one. Treating it as one lets a
			// single stray quote -- an explanation that opens a quotation and
			// never closes it -- swallow the value that follows, so Scan
			// reports one candidate where there were two and DecodeOne hands
			// back an answer instead of refusing an ambiguous reply. The
			// quotes are counted instead, including the ones in the stretch of
			// prose just jumped over: the parity is used below, and only to
			// report truncation, but losing it here would lose the signal that
			// a reply stops inside a quotation. Stray closing brackets in the
			// same stretch have nothing to close and need no counting.
			looseQuotes += strings.Count(text[i:j], `"`)
			if j == len(text) {
				break
			}
			start = j
			stack = append(stack, text[j])
			i = j + 1
			continue
		}
		switch c := text[i]; c {
		case '"':
			// Inside a value a quote opens a real JSON string, where a bracket
			// is content rather than structure.
			e := endOfString(text, i+1)
			if e < 0 {
				// The string never closes, so everything after it is inside a
				// literal that never ends: nothing further can close the value
				// that is still open, and the open stack below reports it.
				i = len(text)
				continue
			}
			i = e
		case '{', '[':
			stack = append(stack, c)
			i++
		case '}', ']':
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
				i++
				continue
			}
			stack = stack[:len(stack)-1]
			i++
			if len(stack) == 0 {
				if !yield(Value{JSON: text[start:i], Start: start, End: i}) {
					return truncated
				}
				start = -1
			}
		default:
			i++
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
	return truncated
}

// indexFrom is the offset of the next c at or after i, or len(s) if there is
// none — a "nothing left" that stays greater than every index, so callers can
// compare it without a second sentinel.
func indexFrom(s string, c byte, i int) int {
	k := strings.IndexByte(s[i:], c)
	if k < 0 {
		return len(s)
	}
	return i + k
}

// endOfString returns the offset just past the quote that closes the string
// whose contents begin at i, or -1 if it is never closed. A quote closes the
// string when an even number of backslashes immediately precedes it, which is
// the same rule as walking the literal with an "escaped" flag and lets the
// search for the quote itself be a memchr.
func endOfString(s string, i int) int {
	for i < len(s) {
		q := indexFrom(s, '"', i)
		if q == len(s) {
			return -1
		}
		b := q
		for b > i && s[b-1] == '\\' {
			b--
		}
		if (q-b)%2 == 0 {
			return q + 1
		}
		i = q + 1
	}
	return -1
}

// First returns the first candidate that is valid JSON, and false if there is
// none. It does not consider the text as a whole; use Decode for that.
func First(text string) (string, bool) {
	out, ok := "", false
	scanValues(text, func(c Value) bool {
		if json.Valid([]byte(c.JSON)) {
			out, ok = c.JSON, true
			return false
		}
		return true
	})
	return out, ok
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
	if trimmed := strings.TrimSpace(text); opensAValue(trimmed) && decodeWhole(trimmed, v, false) == nil {
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
			if decodeExact(cand, scratch, false) == nil {
				matches = append(matches, cand)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return ErrNoJSON
	case 1:
		return decodeExact(matches[0], v, false)
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
	// The shape a model reply actually has, taken in one pass. It answers
	// only when it has decoded what the two steps below would have decoded,
	// and declines everything else; see fastPath.
	if !strict && useFast && fastPath(text, rv.Elem()) {
		return nil
	}
	if trimmed := strings.TrimSpace(text); opensAValue(trimmed) && decodeWhole(trimmed, v, strict) == nil {
		return nil
	}
	found := false
	scanValues(text, func(c Value) bool {
		if decodeExact(c.JSON, v, strict) == nil {
			found = true
			return false
		}
		return true
	})
	if found {
		return nil
	}
	return ErrNoJSON
}

// opensAValue reports whether s could be a JSON value at all: it has to begin
// like one. A reply that opens with prose cannot, and saying so costs a byte
// comparison where the whole-text attempt it skips costs a parse of the entire
// reply — a parse that, for the shape this package exists for, was always
// going to fail. The set is every byte JSON may start a value with, so no text
// that would have decoded is turned away.
func opensAValue(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '{', '[', '"', 't', 'f', 'n', '-',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

// errNotWhole says the text holds a value but is not one: something follows it.
var errNotWhole = errors.New("salvage: trailing data")

// decodeWhole decodes s — trimmed, non-empty, and opening like a JSON value —
// into v only if the WHOLE of s is that one value. Anything after it means the
// text is not itself a value and its candidates must be tried instead, so
// "{...} trailing prose" is a candidate's job, not the whole text's.
func decodeWhole(s string, v any, strict bool) error {
	if s[0] != '{' && s[0] != '[' {
		return decodeScalarWhole(s, v, strict)
	}
	// A bracketed value ends at the bracket that closes it, which the scanner
	// already finds in one pass over the structure; asking encoding/json where
	// it ends means parsing the whole thing just to be told.
	start, end := -1, -1
	scanValues(s, func(c Value) bool {
		start, end = c.Start, c.End
		return false
	})
	if start != 0 {
		return errNotWhole
	}
	if strings.TrimSpace(s[end:]) != "" {
		return errNotWhole
	}
	return decodeExact(s[:end], v, strict)
}

// decodeScalarWhole is decodeWhole for a text that is a bare number, string,
// true, false or null. There is no bracket to match, so encoding/json reports
// where the value ends, and everything after it must be blank.
//
// json.Decoder.More reports whether another element follows INSIDE the array
// or object being parsed, so at the top level it answers false for a leading
// ']' or '}'. Using it as a trailing-data guard therefore accepts `{"a":1}}`
// and `{"a":1}] [{"b":2}]` as a single value, which is how a reply holding two
// answers reaches a caller that asked DecodeOne to refuse exactly that.
// Measure against the input instead: everything after the value must be blank.
func decodeScalarWhole(s string, v any, strict bool) error {
	dec := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if rest := strings.TrimSpace(s[dec.InputOffset():]); rest != "" {
		return errNotWhole
	}
	return decodeExact(string(raw), v, strict)
}

// decodeExact decodes s, which the caller has already established is exactly
// one JSON value with nothing after it, into v.
//
// The extent being known is what makes this cheap: the path this replaced
// parsed every candidate twice, once into a json.RawMessage to find where it
// ended and reject what followed, and then again out of those same bytes into
// the destination. A balanced candidate cannot have anything after its closing
// bracket — that is what balanced means — so the first parse was buying an
// answer the scanner already had.
//
// Decoding goes into a fresh value that is assigned only on success, so a
// candidate that fails half-way never leaves v partly filled for the next one.
func decodeExact(s string, v any, strict bool) error {
	dst := reflect.ValueOf(v).Elem()
	if !strict && useFast && fastExact(s, dst) {
		return nil
	}
	fresh := reflect.New(dst.Type())
	if strict {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.DisallowUnknownFields()
		if err := dec.Decode(fresh.Interface()); err != nil {
			return err
		}
	} else if err := json.Unmarshal([]byte(s), fresh.Interface()); err != nil {
		return err
	}
	dst.Set(fresh.Elem())
	return nil
}
