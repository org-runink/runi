// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The fast path is a shortcut through Decode, not a second opinion about what
// a reply means, so everything about it is tested the same way: run the input
// through both paths and insist they agree. A shortcut that disagrees with the
// code it replaces is not faster, it is wrong.

// bothWays decodes text into two fresh values of the same type, once with the
// fast path and once without, and reports any difference in either the error
// or the value. It returns the error the fast path gave, for tests that also
// care which one it was.
func bothWays(t *testing.T, text string, mk func() any) error {
	t.Helper()
	fast := mk()
	errFast := Decode(text, fast)

	useFast = false
	slow := mk()
	errSlow := Decode(text, slow)
	useFast = true

	if (errFast == nil) != (errSlow == nil) {
		t.Fatalf("fast path err = %v, general path err = %v, for %q", errFast, errSlow, text)
	}
	if !reflect.DeepEqual(fast, slow) {
		t.Fatalf("fast path decoded %+v, general path decoded %+v, from %q", fast, slow, text)
	}
	return errFast
}

type scalars struct {
	S    string  `json:"s"`
	B    bool    `json:"b"`
	I    int     `json:"i"`
	I8   int8    `json:"i8"`
	U    uint    `json:"u"`
	U8   uint8   `json:"u8"`
	F    float64 `json:"f"`
	F32  float32 `json:"f32"`
	List []int   `json:"list"` // known to the layout, never set by it
	Any  any     `json:"any"`
	Skip string  `json:"-"`
	Opt  string  `json:"opt,omitempty"`
	Bare string
	tiny int //nolint:unused // encoding/json never fills it, and nor does this
}

func newScalars() any { return &scalars{} }

// Every member shape, each one decoded both ways.
func TestFastPathAgreesWithEncodingJSON(t *testing.T) {
	for name, text := range map[string]string{
		"empty object":      `{}`,
		"all scalars":       `{"s":"x","b":true,"i":-5,"i8":-128,"u":7,"u8":255,"f":1.5e3,"f32":0.25}`,
		"false":             `{"b":false}`,
		"null member":       `{"s":null,"i":null}`,
		"null over a value": `{"s":"kept","s":null}`,
		"unknown scalar":    `{"other":"ignored","s":"x"}`,
		"unknown object":    `{"other":{"a":[1,2,{"b":null}]},"s":"x"}`,
		"unknown array":     `{"other":[],"s":"x"}`,
		"unknown bool":      `{"other":true,"s":"x"}`,
		"unknown null":      `{"other":null,"s":"x"}`,
		"unknown number":    `{"other":-1.5e-3,"s":"x"}`,
		"duplicate key":     `{"i":1,"i":2}`,
		"whitespace":        "{ \"s\" : \"x\" , \"i\" : 1 }",
		"newlines":          "{\n\t\"s\": \"x\"\n}",
		"prose around it":   `Sure! {"s":"x"} Hope that helps.`,
		"fenced":            "```json\n{\"s\":\"x\"}\n```",
		"trailing prose":    `{"s":"x"} and that is all`,
		"two values":        `{"s":"first"} ... {"s":"second"}`,

		// Shapes the fast path refuses and hands back.
		"escape in value":    `{"s":"say \"hi\""}`,
		"escape in key":      `{"s":"x"}`,
		"unicode escape":     `{"s":"é"}`,
		"nested for a field": `{"list":[1,2],"s":"x"}`,
		"object for a field": `{"any":{"deep":1},"s":"x"}`,
		"case-variant key":   `{"S":"x"}`,
		"number for string":  `{"s":1}`,
		"string for number":  `{"i":"1"}`,
		"bool for string":    `{"s":true}`,
		"number for bool":    `{"b":1}`,
		"fraction for int":   `{"i":1.5}`,
		"exponent for int":   `{"i":1e2}`,
		"int overflow":       `{"i8":300}`,
		"uint overflow":      `{"u8":300}`,
		"negative uint":      `{"u":-1}`,
		"huge int":           `{"i":123456789012345678901234567890}`,
		"huge float":         `{"f":1e400}`,
		"float32 overflow":   `{"f32":1e300}`,
		"nineteen digits":    `{"i":1234567890123456789}`,
		"twenty digits":      `{"u":12345678901234567890}`,
		"min int64":          `{"i":-9223372036854775808}`,
		"max int64 plus one": `{"i":9223372036854775808}`,
		"minus zero":         `{"f":-0.0}`,
		"ignored field":      `{"-":"x","s":"y"}`,
		"tagged option":      `{"opt":"x"}`,
		"untagged field":     `{"Bare":"x"}`,
		"unexported name":    `{"tiny":5,"s":"x"}`,

		// Malformed: both paths must refuse.
		"trailing comma":    `{"s":"x",}`,
		"missing colon":     `{"s" "x"}`,
		"missing comma":     `{"s":"x" "i":1}`,
		"unquoted key":      `{s:"x"}`,
		"single quotes":     `{'s':'x'}`,
		"cut off":           `{"s":"x"`,
		"cut mid key":       `{"s`,
		"cut after colon":   `{"s":`,
		"bad literal":       `{"b":tru}`,
		"bad null":          `{"s":nul}`,
		"control character": "{\"s\":\"a\tb\"}",
		"bad escape":        `{"s":"a\qb"}`,
		"short unicode":     `{"s":"\u00"}`,
		"bad unicode":       `{"s":"\u00zz"}`,
		"escape at end":     `{"s":"a\`,
		"leading zero":      `{"i":01}`,
		"leading plus":      `{"i":+1}`,
		"bare dot":          `{"f":.5}`,
		"trailing dot":      `{"f":1.}`,
		"empty exponent":    `{"f":1e}`,
		"exponent sign":     `{"f":1e+}`,
		"lone minus":        `{"i":-}`,
		"not a value":       `{"s":%}`,
		"junk after member": `{"s":"x";"i":1}`,
	} {
		t.Run(name, func(t *testing.T) { bothWays(t, text, newScalars) })
	}
}

// A float field, an unsigned field and a bool all have to arrive intact, which
// the agreement tests above check but do not state.
func TestFastPathValues(t *testing.T) {
	var got scalars
	text := `Here: {"s":"ok","b":true,"i":-5,"i8":-128,"u":7,"u8":255,"f":2.5,"f32":0.5}`
	if err := Decode(text, &got); err != nil {
		t.Fatal(err)
	}
	want := scalars{S: "ok", B: true, I: -5, I8: -128, U: 7, U8: 255, F: 2.5, F32: 0.5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Decode = %+v, want %+v", got, want)
	}
}

// A decoded string must not hold the reply it came out of alive. The fast path
// slices the reply to read the value, and keeping that slice would turn every
// small decoded field into a reference to the whole model reply.
func TestFastPathCopiesStrings(t *testing.T) {
	reply := strings.Repeat("prose ", 200) + `{"s":"small"}`
	var got scalars
	if err := Decode(reply, &got); err != nil {
		t.Fatal(err)
	}
	if got.S != "small" {
		t.Fatalf("S = %q", got.S)
	}
	// A string sliced out of the reply costs nothing to produce and holds the
	// whole reply alive; a copy costs one allocation and holds nothing. There
	// is no portable way to ask which one this is, so count the allocation:
	// without the copy there is none.
	n := testing.AllocsPerRun(50, func() { _ = Decode(reply, &got) })
	if n < 1 {
		t.Errorf("decoding one string allocated %v times: the value is a slice "+
			"of the reply, which keeps the whole reply alive", n)
	}
}

// Destinations the fast path does not take at all. Each one must still decode,
// through encoding/json, exactly as it always did.
func TestFastPathDeclinesAndStillDecodes(t *testing.T) {
	type embedded struct{ A int }

	t.Run("not a struct", func(t *testing.T) {
		bothWays(t, `{"a":1}`, func() any { return &map[string]int{} })
		bothWays(t, `{"a":1}`, func() any { var v any; return &v })
		bothWays(t, `[1,2]`, func() any { return &[]int{} })
	})
	t.Run("embedded field", func(t *testing.T) {
		type outer struct {
			embedded
			B int `json:"b"`
		}
		bothWays(t, `{"A":1,"b":2}`, func() any { return &outer{} })
	})
	t.Run("string option", func(t *testing.T) {
		type opt struct {
			A int `json:"a,string"`
		}
		bothWays(t, `{"a":"12"}`, func() any { return &opt{} })
	})
	t.Run("two fields one name", func(t *testing.T) {
		// Built at run time because the vet check for repeated tags is right:
		// this is a mistake, and the point is that it stays encoding/json's
		// mistake to resolve rather than becoming a second one here.
		dup := reflect.StructOf([]reflect.StructField{
			{Name: "A", Type: reflect.TypeOf(0), Tag: `json:"x"`},
			{Name: "B", Type: reflect.TypeOf(0), Tag: `json:"x"`},
		})
		bothWays(t, `{"x":1}`, func() any { return reflect.New(dup).Interface() })
	})
	t.Run("unicode field name", func(t *testing.T) {
		type greek struct{ Ωmega int }
		bothWays(t, `{"Ωmega":1}`, func() any { return &greek{} })
	})
	t.Run("tag needing encoding/json's rules", func(t *testing.T) {
		type odd struct {
			A int `json:"a b"`
		}
		bothWays(t, `{"a b":1}`, func() any { return &odd{} })
	})
	t.Run("empty tag name", func(t *testing.T) {
		type blank struct {
			A int `json:",omitempty"`
		}
		bothWays(t, `{"A":1}`, func() any { return &blank{} })
	})
	t.Run("custom unmarshaler", func(t *testing.T) {
		bothWays(t, `{"a":1}`, func() any { return &counted{} })
	})
	t.Run("field with a custom unmarshaler", func(t *testing.T) {
		type holder struct {
			C counted `json:"c"`
			S string  `json:"s"`
		}
		bothWays(t, `{"c":1,"s":"x"}`, func() any { return &holder{} })
		bothWays(t, `{"s":"x"}`, func() any { return &holder{} })
	})
	t.Run("too many fields", func(t *testing.T) {
		bothWays(t, `{"F01":1}`, func() any { return &wide{} })
	})
	t.Run("too many members", func(t *testing.T) {
		// More members than the fast path will hold before writing them.
		var b strings.Builder
		b.WriteByte('{')
		for i := 0; i < maxFastAssign+2; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`"i":1`)
		}
		b.WriteByte('}')
		bothWays(t, b.String(), newScalars)

		// The same, member by member, for each kind that is held before it is
		// written: a string, a bool and a number all have to stop there.
		for _, member := range []string{`"s":"x"`, `"b":true`, `"f":1.5`} {
			var many strings.Builder
			many.WriteByte('{')
			for i := 0; i < maxFastAssign+2; i++ {
				if i > 0 {
					many.WriteByte(',')
				}
				many.WriteString(member)
			}
			many.WriteByte('}')
			bothWays(t, many.String(), newScalars)
		}
	})
	t.Run("deeper than the fast path walks", func(t *testing.T) {
		deep := strings.Repeat("[", maxFastDepth+2) + strings.Repeat("]", maxFastDepth+2)
		bothWays(t, `{"other":`+deep+`,"s":"x"}`, newScalars)
	})
	t.Run("an array comes first", func(t *testing.T) {
		bothWays(t, `[{"s":"in a list"}]`, newScalars)
		bothWays(t, `[1,2] then {"s":"x"}`, newScalars)
	})
	t.Run("no object at all", func(t *testing.T) {
		bothWays(t, `nothing here`, newScalars)
		bothWays(t, ``, newScalars)
	})
}

// counted has an UnmarshalJSON of its own, which the fast path must never
// stand in for.
type counted struct{ N int }

func (c *counted) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	c.N = n + 100 // deliberately not what a plain decode would give
	return nil
}

type wide struct {
	F01, F02, F03, F04, F05, F06, F07, F08 int
	F09, F10, F11, F12, F13, F14, F15, F16 int
	F17, F18, F19, F20, F21, F22, F23, F24 int
	F25, F26, F27, F28, F29, F30, F31, F32 int
	F33                                    int
}

// The fast path has to reach the same answer through the entry points that do
// not scan the text themselves.
func TestFastPathThroughDecodeOne(t *testing.T) {
	var v scalars
	if err := DecodeOne(`the answer: {"s":"x","i":3}`, &v); err != nil {
		t.Fatalf("err = %v", err)
	}
	if v.S != "x" || v.I != 3 {
		t.Errorf("v = %+v", v)
	}
	// Two objects that both fit is still ambiguous, fast path or not.
	var v2 scalars
	if err := DecodeOne(`{"s":"a"} or {"s":"b"}`, &v2); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
	if !reflect.DeepEqual(v2, scalars{}) {
		t.Errorf("destination written before refusing: %+v", v2)
	}
	// And a singleton array still unwraps.
	var v3 scalars
	if err := DecodeOne(`[{"s":"inside"}]`, &v3); err != nil || v3.S != "inside" {
		t.Errorf("v = %+v, err = %v", v3, err)
	}
}

// DecodeStrict never takes the fast path: unknown fields are the whole point
// of it, and the fast path ignores them.
func TestFastPathIsNotTakenByDecodeStrict(t *testing.T) {
	var v scalars
	if err := DecodeStrict(`{"s":"x","other":1}`, &v); !errors.Is(err, ErrNoJSON) {
		t.Errorf("err = %v, want ErrNoJSON", err)
	}
	if err := DecodeStrict(`{"s":"x"}`, &v); err != nil || v.S != "x" {
		t.Errorf("v = %+v, err = %v", v, err)
	}
}

// The performance claim, as a test. The shortcut is only worth having if it is
// actually taken for the reply shape it was written for, and nothing about a
// decode that takes it looks different from one that does not: it returns the
// same value and the same nil error. If a condition is reordered, a type stops
// qualifying, or the whole-text attempt creeps back in front of it, this is
// what says so -- where a benchmark would only say it got slower, on a machine
// nobody was watching.
func TestOrdinaryReplyTakesTheFastPath(t *testing.T) {
	reply := "Sure — here is the result you asked for.\n\n```json\n" +
		`{"id":7,"verdict":"pass","score":0.37,"notes":"line 7 of the reply"}` +
		"\n```\n\nLet me know if you need anything else."
	type verdict struct {
		ID      int     `json:"id"`
		Verdict string  `json:"verdict"`
		Score   float64 `json:"score"`
		Notes   string  `json:"notes"`
	}

	var direct verdict
	if !fastPath(reply, reflect.ValueOf(&direct).Elem()) {
		t.Fatal("the fast path declined an ordinary model reply")
	}
	want := verdict{ID: 7, Verdict: "pass", Score: 0.37, Notes: "line 7 of the reply"}
	if direct != want {
		t.Fatalf("fast path decoded %+v, want %+v", direct, want)
	}

	// And it costs less than the path it replaces. Bytes, not time: the
	// difference is the parse into a json.RawMessage and the second parse out
	// of it, and those allocate whatever the machine is doing otherwise.
	var v verdict
	fast := allocBytes(200, func() { _ = Decode(reply, &v) })
	useFast = false
	general := allocBytes(200, func() { _ = Decode(reply, &v) })
	useFast = true
	if fast*2 >= general {
		t.Errorf("one reply allocates %d bytes on the fast path and %d on the "+
			"general one: the fast path is not being taken", fast, general)
	}
}

// allocBytes is the bytes f allocates per call. TotalAlloc counts every
// allocation ever made, so this does not depend on when a collection happens.
func allocBytes(n int, f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < n; i++ {
		f()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(n)
}

// Decoding a reply that is only prose must not parse anything: the whole-text
// attempt it used to make was always going to fail.
func TestProseIsNotParsed(t *testing.T) {
	var v scalars
	n := testing.AllocsPerRun(50, func() { _ = Decode("I could not answer that.", &v) })
	if n != 0 {
		t.Errorf("prose allocated %v times, want 0", n)
	}
}

// Generated objects, generated prose, generated destinations: the fast path
// and encoding/json must reach the same place for all of them. The generator
// aims at the seams -- keys that match, keys that differ only in case, keys
// nobody wants, values of every kind, numbers at the edge of every width.
func TestPropertyFastPathMatchesEncodingJSON(t *testing.T) {
	r := rand.New(rand.NewPCG(53, 54))
	keys := []string{"s", "b", "i", "i8", "u", "u8", "f", "f32", "list", "any",
		"S", "I", "opt", "Bare", "other", "", "-", "tiny", "o\\u0070t"}
	for i := 0; i < 4000; i++ {
		var b strings.Builder
		b.WriteByte('{')
		for n := r.IntN(5); n > 0; n-- {
			if b.Len() > 1 {
				b.WriteByte(',')
			}
			b.WriteByte('"')
			b.WriteString(keys[r.IntN(len(keys))])
			b.WriteString(`":`)
			b.WriteString(randMember(r, 2))
		}
		b.WriteByte('}')
		text := randString(r) + b.String() + randString(r)
		bothWays(t, text, newScalars)
	}
}

// randMember is a JSON value, drawn towards the ones that decide something:
// numbers that do not fit, strings that need unescaping, nulls.
func randMember(r *rand.Rand, depth int) string {
	switch r.IntN(12) {
	case 0:
		return "null"
	case 1:
		return "true"
	case 2:
		return "false"
	case 3:
		return []string{"0", "-0", "1", "-1", "127", "128", "255", "256",
			"-128", "-129", "1.5", "1e2", "1e400", "-1e-3",
			"9223372036854775807", "9223372036854775808", "-9223372036854775808",
			"12345678901234567890", "01", "1.", ".5", "+1", "1e"}[r.IntN(23)]
	case 4, 5, 6:
		return `"` + randString(r) + `"`
	case 7:
		return `"` + []string{`a\"b`, `é`, `\q`, `\\`, "\x01", `\u00`}[r.IntN(6)] + `"`
	case 8:
		if depth == 0 {
			return "1"
		}
		return "[" + randMember(r, depth-1) + "," + randMember(r, depth-1) + "]"
	case 9:
		if depth == 0 {
			return "1"
		}
		return `{"k":` + randMember(r, depth-1) + "}"
	case 10:
		return "[]"
	default:
		return "{}"
	}
}

// FuzzFastPath: whatever the text, the shortcut and the code it replaces end
// in the same place with the same value.
func FuzzFastPath(f *testing.F) {
	for _, s := range []string{
		`{"s":"x"}`, `prose {"i":1} prose`, `{"s":"é"}`, `{"S":1}`,
		`{"other":{"a":[1,{"b":2}]},"b":true}`, `{`, `{}`, `[{"s":"x"}]`,
		`{"i":1e2}`, `{"f":1.5}`, "{\"s\":\"\x01\"}", `{"s":"` + "\xff" + `"}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		bothWays(t, s, newScalars)
		bothWays(t, s, func() any { var v any; return &v })
	})
}

// The pieces of the parser that the entry points above cannot reach on their
// own, because nothing that gets that far is ever this badly formed.
func TestParserEdges(t *testing.T) {
	if _, _, ok := scanNumber("", 0); ok {
		t.Error("an empty number was accepted")
	}
	if _, ok := skipValue("1", 0, maxFastDepth+1); ok {
		t.Error("nesting past the limit was accepted")
	}
	if _, ok := skipValue("", 0, 0); ok {
		t.Error("an empty value was accepted")
	}
	for _, s := range []string{`[1`, `[1,`, `[1 2]`, `[,]`, `{"a":1`, `{"a"`,
		`{"a":`, `{"a":1 2}`, `{1:2}`, `{"a` + "\x01" + `":1}`, `[[1]`,
		`[tru]`, `[fals]`, `[nul]`, `{"a":tru}`} {
		if _, ok := skipValue(s, 0, 0); ok {
			t.Errorf("skipValue accepted %q", s)
		}
	}
	for _, s := range []string{`[]`, `[1,2]`, `[[1],{"a":[true,false,null]}]`,
		`{}`, `{"a" : 1 , "b" : "c"}`, `"x"`, `-1.5e+3`, `0`} {
		if end, ok := skipValue(s, 0, 0); !ok || end != len(s) {
			t.Errorf("skipValue(%q) = %d, %v", s, end, ok)
		}
	}
	if !hex4("00ff") || !hex4("AbCd") || hex4("00g0") {
		t.Error("hex4")
	}
	// A name is compared as bytes only when it is made of ordinary ones. No
	// field reaches this empty, and a name that did would match no key.
	if plainName("") || !plainName("a_b-c.d9") || plainName("a b") {
		t.Error("plainName")
	}
	if got := skipSpace("   ", 0); got != 3 {
		t.Errorf("skipSpace = %d", got)
	}
}

// parseInt and parseUint stand in for strconv on the literals the JSON number
// grammar accepts, and must agree with it on every one of them -- including
// the ones they hand back.
func TestPropertyIntegerParsingMatchesStrconv(t *testing.T) {
	r := rand.New(rand.NewPCG(55, 56))
	lits := []string{"0", "-0", "1", "-1", "9", "10", "99", "100", "127", "128",
		"-128", "-129", "255", "256", "32767", "-32768", "65535",
		"2147483647", "-2147483648", "4294967295",
		"9223372036854775807", "-9223372036854775808", "9223372036854775808",
		"18446744073709551615", "18446744073709551616", "1234567890123456789",
		"12345678901234567890", "1.5", "1e2", "1E-2", "-1.5e+3", "0.0"}
	for i := 0; i < 20000; i++ {
		lit := lits[r.IntN(len(lits))]
		if r.IntN(4) == 0 {
			lit = strings.Repeat("9", 1+r.IntN(22))
			if r.IntN(2) == 0 {
				lit = "-" + lit
			}
		}
		// Only a literal the grammar accepts whole ever reaches these.
		if got, end, ok := scanNumber(lit, 0); !ok || end != len(lit) || got != lit {
			continue
		}
		for _, bits := range []int{8, 16, 32, 64} {
			want, wantErr := strconv.ParseInt(lit, 10, bits)
			got, ok := parseInt(lit, bits)
			if ok != (wantErr == nil) || (ok && got != want) {
				t.Fatalf("parseInt(%q, %d) = %d, %v; strconv = %d, %v",
					lit, bits, got, ok, want, wantErr)
			}
			uwant, uwantErr := strconv.ParseUint(lit, 10, bits)
			ugot, uok := parseUint(lit, bits)
			if uok != (uwantErr == nil) || (uok && ugot != uwant) {
				t.Fatalf("parseUint(%q, %d) = %d, %v; strconv = %d, %v",
					lit, bits, ugot, uok, uwant, uwantErr)
			}
		}
	}
}

func TestFloatBitsRoundTrip(t *testing.T) {
	for _, f := range []float64{0, -0, 1.5, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		if got := math.Float64frombits(math.Float64bits(f)); got != f {
			t.Errorf("%v round-tripped to %v", f, got)
		}
	}
}
