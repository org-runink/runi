package salvage

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
)

// salvage exists to pull JSON out of text a model wrapped in prose, and its
// one unforgivable failure is returning a fragment that decodes cleanly and
// means something else. These properties are written against generated JSON
// and generated truncations, because a truncation that matters is one nobody
// thought to write down.

// randJSON builds an arbitrary JSON value: nested objects, arrays, strings
// with quotes and backslashes in them, numbers, booleans and nulls.
func randJSON(r *rand.Rand, depth int) any {
	if depth <= 0 {
		switch r.IntN(4) {
		case 0:
			return randString(r)
		case 1:
			return r.NormFloat64() * 100
		case 2:
			return r.IntN(2) == 0
		default:
			return nil
		}
	}
	switch r.IntN(6) {
	case 0, 1:
		m := map[string]any{}
		for i := 0; i < r.IntN(4); i++ {
			m[randString(r)] = randJSON(r, depth-1)
		}
		return m
	case 2, 3:
		a := make([]any, r.IntN(4))
		for i := range a {
			a[i] = randJSON(r, depth-1)
		}
		return a
	default:
		return randJSON(r, 0)
	}
}

// randString deliberately includes the characters that break a naive scanner:
// quotes, backslashes, and brackets inside string literals.
func randString(r *rand.Rand) string {
	alphabet := []rune(`abc {}[]"\` + "\n\t,:")
	n := r.IntN(8)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(alphabet[r.IntN(len(alphabet))])
	}
	return b.String()
}

// Scan's offsets must always agree with the JSON it reports. A caller that
// re-slices the input using Start and End must get the same bytes back.
func TestPropertyScanOffsetsAlwaysMatchTheText(t *testing.T) {
	r := rand.New(rand.NewPCG(41, 42))
	for i := 0; i < 3000; i++ {
		v := randJSON(r, 3)
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		text := randString(r) + string(raw) + randString(r)
		vals, _ := Scan(text)
		for _, got := range vals {
			if got.Start < 0 || got.End > len(text) || got.Start >= got.End {
				t.Fatalf("offsets out of range: %+v in %q", got, text)
			}
			if text[got.Start:got.End] != got.JSON {
				t.Fatalf("JSON %q != text[%d:%d] %q", got.JSON, got.Start, got.End, text[got.Start:got.End])
			}
		}
	}
}

// Scan's values never overlap and always come out in order. A caller taking
// the first, or iterating them, must not see the same bytes twice.
func TestPropertyScanValuesAreDisjointAndOrdered(t *testing.T) {
	r := rand.New(rand.NewPCG(43, 44))
	for i := 0; i < 3000; i++ {
		var parts []string
		for j := 0; j < 1+r.IntN(4); j++ {
			raw, err := json.Marshal(randJSON(r, 2))
			if err != nil {
				continue
			}
			parts = append(parts, randString(r), string(raw))
		}
		text := strings.Join(parts, " ")
		vals, _ := Scan(text)
		for j := 1; j < len(vals); j++ {
			if vals[j].Start < vals[j-1].End {
				t.Fatalf("value %d starts at %d, inside the one ending at %d", j, vals[j].Start, vals[j-1].End)
			}
		}
	}
}

// Any JSON object or array, wrapped in any prose, round-trips: Scan finds it
// and Decode reproduces the value it was marshalled from.
func TestPropertyAnyWrappedValueRoundTrips(t *testing.T) {
	r := rand.New(rand.NewPCG(45, 46))
	for i := 0; i < 3000; i++ {
		v := randJSON(r, 3)
		switch v.(type) {
		case map[string]any, []any:
		default:
			continue // Scan looks for bracketed values, by design
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		text := "Sure, here you go:\n```json\n" + string(raw) + "\n```\nHope that helps."

		var got any
		if err := Decode(text, &got); err != nil {
			t.Fatalf("Decode failed on %q: %v", raw, err)
		}
		reraw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		// Compare through a canonical re-marshal: map order is not meaning.
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		wantraw, _ := json.Marshal(want)
		if string(reraw) != string(wantraw) {
			t.Fatalf("round trip changed the value:\n in: %s\nout: %s", wantraw, reraw)
		}
	}
}

// THE property this package exists for: a reply cut off mid-value must never
// hand back a complete inner fragment as though it were the whole answer.
// Every truncation of a nested value either reports truncated, or returns
// nothing at all -- never a confident wrong answer.
func TestPropertyEveryTruncationFailsClosed(t *testing.T) {
	r := rand.New(rand.NewPCG(47, 48))
	checked := 0
	for i := 0; i < 4000 && checked < 2000; i++ {
		// A wrapper object around a list of complete objects: the exact shape
		// that goes wrong, because the inner objects survive the cut.
		inner := make([]any, 1+r.IntN(4))
		for j := range inner {
			inner[j] = map[string]any{"id": j, "note": randString(r)}
		}
		raw, err := json.Marshal(map[string]any{"findings": inner})
		if err != nil {
			continue
		}
		full := string(raw)
		cut := 1 + r.IntN(len(full)-1) // strictly inside
		text := full[:cut]
		checked++

		vals, truncated := Scan(text)
		if truncated {
			continue // reported, which is all that is asked
		}
		// Not reported as truncated, so nothing returned may claim to be the
		// whole reply.
		for _, got := range vals {
			if got.Start == 0 && got.End == len(text) {
				t.Fatalf("a %d-byte cut of a %d-byte reply was returned whole and NOT flagged: %q",
					cut, len(full), got.JSON)
			}
		}
	}
}

// strayProse is filler for the text around a value, drawn from characters that
// cannot merge two values into one: letters, punctuation, and closing brackets
// with nothing to close. An OPENING bracket in the filler legitimately makes
// the two objects one larger (invalid) candidate, so it is left out here --
// that behaviour is covered by the scan properties above. The closers are kept
// deliberately: `}` and `]` between two values are what exposed the
// trailing-data guard accepting two answers as one.
func strayProse(r *rand.Rand) string {
	alphabet := []rune("abc :,}]" + "\n\t")
	var b strings.Builder
	for i := 0; i < r.IntN(8); i++ {
		b.WriteRune(alphabet[r.IntN(len(alphabet))])
	}
	return b.String()
}

// DecodeOne must refuse when the text holds more than one candidate, and must
// leave the destination untouched whenever it refuses. A half-filled struct is
// worse than no struct, because the caller cannot tell.
func TestPropertyDecodeOneNeverPartiallyFills(t *testing.T) {
	r := rand.New(rand.NewPCG(49, 50))
	type dest struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	for i := 0; i < 2000; i++ {
		one, _ := json.Marshal(map[string]any{"a": 1 + r.IntN(100), "b": randString(r)})
		two, _ := json.Marshal(map[string]any{"a": 1 + r.IntN(100), "b": randString(r)})
		text := strayProse(r) + string(one) + strayProse(r) + string(two)

		sentinel := dest{A: -7, B: "untouched"}
		got := sentinel
		if err := DecodeOne(text, &got); err == nil {
			t.Fatalf("DecodeOne accepted two candidates: %q", text)
		}
		if got != sentinel {
			t.Fatalf("DecodeOne wrote to the destination before failing: %+v", got)
		}
	}
}

// First and Scan must agree. First is documented as the first candidate that
// is VALID JSON, not simply the first balanced one, so the property is that it
// equals the first value Scan found that json.Valid accepts -- and that it
// reports nothing exactly when Scan found no valid value.
func TestPropertyFirstIsTheFirstValidScanValue(t *testing.T) {
	r := rand.New(rand.NewPCG(51, 52))
	for i := 0; i < 3000; i++ {
		raw, err := json.Marshal(randJSON(r, 3))
		if err != nil {
			continue
		}
		text := randString(r) + string(raw) + randString(r)

		want, wantOK := "", false
		vals, _ := Scan(text)
		for _, v := range vals {
			if json.Valid([]byte(v.JSON)) {
				want, wantOK = v.JSON, true
				break
			}
		}
		got, ok := First(text)
		if ok != wantOK || got != want {
			t.Fatalf("First = %q (%v), first valid Scan value = %q (%v), text %q",
				got, ok, want, wantOK, text)
		}
	}
}

// Scan now jumps: outside a value it goes to the next opening bracket with
// IndexByte instead of reading the prose a byte at a time, and inside a string
// literal it goes to the closing quote the same way. Both jumps skip bytes the
// old loop looked at, and one of those bytes carried meaning -- a quote in the
// prose, counted for its parity, which is how a reply that stops inside a
// quotation is reported as truncated. scanReference is the loop as it was, and
// the property is that the two agree about everything, on any text.
func scanReference(text string) (vals []Value, truncated bool) {
	var stack []byte
	start := -1
	inString, escaped := false, false
	looseQuotes := 0

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
				looseQuotes++
				continue
			}
			inString = true
		case '{', '[':
			if len(stack) == 0 {
				start = i
			}
			stack = append(stack, c)
		case '}', ']':
			if len(stack) == 0 {
				continue
			}
			want := byte('}')
			if stack[len(stack)-1] == '[' {
				want = ']'
			}
			if c != want {
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
	if len(stack) > 0 || looseQuotes%2 == 1 {
		truncated = true
	}
	return vals, truncated
}

func sameScan(t *testing.T, text string) {
	t.Helper()
	got, gotTrunc := Scan(text)
	want, wantTrunc := scanReference(text)
	if gotTrunc != wantTrunc {
		t.Fatalf("truncated = %v, a byte at a time it is %v, for %q", gotTrunc, wantTrunc, text)
	}
	if len(got) != len(want) {
		t.Fatalf("Scan found %d values, a byte at a time it finds %d, for %q", len(got), len(want), text)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("value %d is %+v, a byte at a time it is %+v, for %q", i, got[i], want[i], text)
		}
	}
}

func TestPropertyScanMatchesAByteAtATime(t *testing.T) {
	r := rand.New(rand.NewPCG(57, 58))
	// The pieces are chosen for the seams: lone quotes, escaped quotes,
	// brackets that never close, closers with nothing to close, and real JSON.
	pieces := []string{`"`, `\"`, `"\\"`, `{`, `}`, `[`, `]`, `{"a":"}"`, ` `,
		"\n", `abc`, `:`, `,`, `{"a":1}`, `[1,2]`, `{"a":[1,{"b":"]"}]}`,
		`"unclosed`, `{"a": [}]}`, `\`, `""`}
	for i := 0; i < 6000; i++ {
		var b strings.Builder
		for n := r.IntN(10); n > 0; n-- {
			if r.IntN(5) == 0 {
				b.WriteString(randString(r))
				continue
			}
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		text := b.String()
		sameScan(t, text)
		// And every prefix of it, which is what a cut-off reply is.
		if cut := r.IntN(len(text) + 1); cut < len(text) {
			sameScan(t, text[:cut])
		}
	}
}

// FuzzScan: the jumps and the byte-at-a-time loop agree on arbitrary input.
func FuzzScan(f *testing.F) {
	for _, s := range []string{"", `"`, `{"a":"}"}`, `{"a": [}]}`, `x "y {"z":1}`,
		`{"a":"\\"}`, `[[{}]]`, "{\"a\":\"\\\"", `}{`, `"a" "b" {"c":1}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { sameScan(t, s) })
}
