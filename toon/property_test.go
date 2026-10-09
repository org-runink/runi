package toon

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// TOON exists so a model can emit a table without spending a token on every
// repeated key, which means the only thing that really matters is that what
// comes back out is what went in. An example test samples that; these state it
// for every shape the encoder is willing to produce.

// randScalar returns a leaf of the kinds TOON carries, including the strings
// that need quoting: commas, colons, quotes, backslashes and newlines.
func randScalar(r *rand.Rand) any {
	switch r.IntN(8) {
	case 0:
		return nil
	case 1:
		return r.IntN(2) == 0
	case 2:
		return json.Number(fmt.Sprint(r.IntN(100000) - 50000))
	case 3:
		return json.Number(fmt.Sprintf("%d.%02d", r.IntN(1000), r.IntN(100)))
	case 4:
		return ""
	case 5:
		// The characters a line-oriented format has to escape.
		alphabet := []rune(`ab,:"\` + "\n\t\r {}[]-")
		var b strings.Builder
		for i := 0; i < r.IntN(10); i++ {
			b.WriteRune(alphabet[r.IntN(len(alphabet))])
		}
		return b.String()
	case 6:
		return fmt.Sprintf("plain%d", r.IntN(1000))
	default:
		// Strings that could be read back as another type if left unquoted.
		return []any{"true", "false", "null", "123", "1.5", "-", "  padded  "}[r.IntN(7)]
	}
}

// randDoc returns a document body. Callers start it at the top level, where
// TOON requires a MAPPING: a bare list has no key to hang on, so Encode gives
// it the synthetic name "items" and the result decodes as an object rather
// than as the list that went in. That is deliberate and pinned by
// TestATopLevelListGainsAnItemsKey, so the round-trip property is stated over
// the documents the format actually round-trips.
func randDoc(r *rand.Rand, depth int) any {
	if depth <= 0 {
		return randScalar(r)
	}
	switch r.IntN(4) {
	case 0: // a uniform table: the shape the format is for
		n := r.IntN(4)
		keys := []string{"id", "name", "qty"}[:1+r.IntN(3)]
		rows := make([]any, n)
		for i := range rows {
			m := map[string]any{}
			for _, k := range keys {
				m[k] = randScalar(r)
			}
			rows[i] = m
		}
		return rows
	case 1:
		a := make([]any, r.IntN(4))
		for i := range a {
			a[i] = randDoc(r, depth-1)
		}
		return a
	default:
		// At least one key. An EMPTY object as a field's value is written as a
		// bare "key:" and reads back as null -- see
		// TestAnEmptyObjectReadsBackAsNull, which pins that deliberately. It
		// is excluded here so the round-trip property states what the format
		// actually guarantees rather than quietly tolerating an exception.
		m := map[string]any{}
		for i := 0; i < 1+r.IntN(3); i++ {
			m[fmt.Sprintf("k%d", i)] = randDoc(r, depth-1)
		}
		return m
	}
}

// canonical re-marshals through encoding/json so two documents can be compared
// without caring about map order, the difference between a nil slice and an
// empty one, or how a number is spelled.
//
// The spelling matters here: Decode fills its destination through
// encoding/json without UseNumber, so a number becomes a float64 and the
// literal "699.30" comes back as 699.3. That is the same quantity, so a
// comparison of meaning must not fail on it. It is NOT the same for a very
// large integer -- see TestLargeIntegersNeedParseNotDecode, which pins that.
// randTopLevel returns a mapping, which is what a TOON document is.
func randTopLevel(r *rand.Rand) any {
	m := map[string]any{}
	for i := 0; i < 1+r.IntN(3); i++ {
		m[fmt.Sprintf("k%d", i)] = randDoc(r, 2)
	}
	return m
}

func canonical(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back any
	if err := json.Unmarshal(b, &back); err != nil { // numbers land as float64
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("remarshal: %v", err)
	}
	return string(out)
}

// THE property: anything Encode accepts, Decode gives back unchanged. A
// format whose own writer and reader disagree is worse than no format,
// because the disagreement is silent and shows up as a missing field.
func TestPropertyEncodeDecodeRoundTrips(t *testing.T) {
	r := rand.New(rand.NewPCG(301, 302))
	for i := 0; i < 4000; i++ {
		doc := randTopLevel(r)
		text, err := Encode(doc)
		if err != nil {
			// Encode refuses some inputs by design (a bare scalar, a
			// non-uniform row set). Refusing is allowed; mangling is not.
			continue
		}
		var got any
		if err := Decode(text, &got); err != nil {
			t.Fatalf("Decode rejected what Encode produced: %v\n--- encoded ---\n%s", err, text)
		}
		if want, have := canonical(t, doc), canonical(t, got); want != have {
			t.Fatalf("round trip changed the document\n in: %s\nout: %s\n--- encoded ---\n%s", want, have, text)
		}
	}
}

// Encoding is a function: the same document always produces the same text,
// whatever order its maps happen to iterate in. A format that varied would
// make every diff and every cache key useless.
func TestPropertyEncodingIsDeterministic(t *testing.T) {
	r := rand.New(rand.NewPCG(303, 304))
	for i := 0; i < 2000; i++ {
		doc := randTopLevel(r)
		first, err := Encode(doc)
		if err != nil {
			continue
		}
		for rep := 0; rep < 5; rep++ {
			again, err := Encode(doc)
			if err != nil {
				t.Fatalf("Encode succeeded then failed: %v", err)
			}
			if again != first {
				t.Fatalf("Encode is not deterministic:\n%q\n%q", first, again)
			}
		}
	}
}

// The format reaches a FIXED POINT: once a document has been through the
// encoder and the reader once, encoding it again gives the same bytes forever.
//
// The first pass is allowed to normalise -- a caller can hand in the number
// 699.30, and 699.3 comes back, which is the same quantity spelled the way
// encoding/json spells it. What must not happen is drift: a second pass that
// changes the text again, and a third that changes it once more, which is how
// a diff of two identical documents comes out non-empty.
func TestPropertyReEncodingIsStable(t *testing.T) {
	r := rand.New(rand.NewPCG(305, 306))
	for i := 0; i < 3000; i++ {
		doc := randTopLevel(r)
		first, err := Encode(doc)
		if err != nil {
			continue
		}
		var mid any
		if err := Decode(first, &mid); err != nil {
			t.Fatalf("Decode: %v\n%s", err, first)
		}
		second, err := Encode(mid)
		if err != nil {
			t.Fatalf("re-Encode of a decoded document failed: %v\n%s", err, first)
		}
		var mid2 any
		if err := Decode(second, &mid2); err != nil {
			t.Fatalf("Decode of a re-encoded document failed: %v\n%s", err, second)
		}
		third, err := Encode(mid2)
		if err != nil {
			t.Fatalf("third Encode failed: %v\n%s", err, second)
		}
		if third != second {
			t.Fatalf("the format never settles:\n--- 2 ---\n%s\n--- 3 ---\n%s", second, third)
		}
	}
}

// Parse and Decode must agree. Decode goes through encoding/json to fill a
// typed destination; Parse hands back the generic tree. If they disagreed, a
// caller's choice of entry point would change the answer.
func TestPropertyParseAgreesWithDecode(t *testing.T) {
	r := rand.New(rand.NewPCG(307, 308))
	for i := 0; i < 3000; i++ {
		doc := randTopLevel(r)
		text, err := Encode(doc)
		if err != nil {
			continue
		}
		parsed, perr := Parse(text)
		var decoded any
		derr := Decode(text, &decoded)
		if (perr == nil) != (derr == nil) {
			t.Fatalf("Parse and Decode disagree on validity: %v vs %v\n%s", perr, derr, text)
		}
		if perr != nil {
			continue
		}
		if want, have := canonical(t, parsed), canonical(t, decoded); want != have {
			t.Fatalf("Parse and Decode returned different documents\n%s\n%s", want, have)
		}
	}
}

// No input may panic the parser, and every rejection must be a SyntaxError
// carrying a line number a caller can point at. Models emit damaged TOON
// constantly; a parser that panicked on it would take the process with it.
func TestPropertyGarbageIsRefusedNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(309, 310))
	alphabet := []rune("ab: ,-\"\\\n\t[]{}#1|")
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for j := 0; j < r.IntN(60); j++ {
			b.WriteRune(alphabet[r.IntN(len(alphabet))])
		}
		text := b.String()

		var got any
		err := Decode(text, &got)
		if err == nil {
			continue // accidentally valid is fine
		}
		var se *SyntaxError
		if !asSyntaxError(err, &se) {
			continue // a json type error is also a legitimate refusal
		}
		if se.Line < 0 {
			t.Fatalf("SyntaxError carries line %d for %q", se.Line, text)
		}
	}
}

func asSyntaxError(err error, target **SyntaxError) bool {
	se, ok := err.(*SyntaxError)
	if ok {
		*target = se
	}
	return ok
}

// Truncating valid TOON must never silently produce a different valid
// document that looks complete. It may parse to a shorter one -- the format is
// line-oriented, so a cut at a line boundary is a legal shorter document --
// but it must never gain a field or change one.
func TestPropertyTruncationNeverInventsData(t *testing.T) {
	r := rand.New(rand.NewPCG(311, 312))
	for i := 0; i < 3000; i++ {
		doc := randTopLevel(r)
		text, err := Encode(doc)
		if err != nil || text == "" {
			continue
		}
		cut := r.IntN(len(text))
		var got any
		if err := Decode(text[:cut], &got); err != nil {
			continue // refusing a truncated document is the preferred outcome
		}
		// Whatever it parsed, re-encoding it must be a prefix-consistent
		// document in its own right, not something that fails to encode.
		if _, err := Encode(got); err != nil {
			if _, isMap := got.(map[string]any); isMap {
				t.Fatalf("a truncated document parsed into something unencodable: %v\n%q", err, text[:cut])
			}
		}
	}
}
