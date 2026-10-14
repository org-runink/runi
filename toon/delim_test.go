package toon

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// The delimiter is declared in each header, so the round trip has to hold for
// every one of them and for every shape, not only for the comma it was first
// written with.
func TestPropertyRoundTripsWithEveryDelimiter(t *testing.T) {
	r := rand.New(rand.NewPCG(951, 952))
	for _, d := range []rune{',', '\t', '|'} {
		for i := 0; i < 3000; i++ {
			doc := randTopLevel(r)
			text, err := EncodeWith(doc, Options{Delimiter: d})
			if err != nil {
				continue // the documented refusals: an empty list in a list
			}
			var back any
			if err := Strict(text, &back); err != nil {
				t.Fatalf("delimiter %q: decode: %v\n%s", d, err, text)
			}
			if got, want := canonical(t, back), canonical(t, doc); got != want {
				t.Fatalf("delimiter %q changed the value\n got: %s\nwant: %s\n%s", d, got, want, text)
			}
		}
	}
}

// The header spells the delimiter the way the spec does -- inside the
// brackets, absent for comma -- and the field list and the rows use it.
func TestHeadersDeclareTheDelimiter(t *testing.T) {
	doc := map[string]any{
		"rows": []any{
			map[string]any{"id": 1, "note": "Verify, then approve"},
			map[string]any{"id": 2, "note": "ship"},
		},
		"tags": []any{"a", "b", "c"},
	}
	for d, want := range map[rune]string{
		',':  "rows[2]{id,note}:\n  1,\"Verify, then approve\"\n  2,ship\ntags[3]: a,b,c\n",
		'\t': "rows[2\t]{id\tnote}:\n  1\tVerify, then approve\n  2\tship\ntags[3\t]: a\tb\tc\n",
		'|':  "rows[2|]{id|note}:\n  1|Verify, then approve\n  2|ship\ntags[3|]: a|b|c\n",
	} {
		got, err := EncodeWith(doc, Options{Delimiter: d})
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("delimiter %q:\n got %q\nwant %q", d, got, want)
		}
	}
}

// A value is quoted when it contains the ACTIVE delimiter and only then: a
// comma costs two quotes under the comma delimiter and nothing under tab,
// while a pipe has to be quoted under the pipe delimiter.
func TestQuotingFollowsTheDelimiter(t *testing.T) {
	for _, tc := range []struct {
		s     string
		delim byte
		quote bool
	}{
		{"a,b", ',', true}, {"a,b", '\t', false}, {"a,b", '|', false},
		{"a|b", ',', false}, {"a|b", '|', true}, {"a|b", '\t', false},
		{"a\tb", '\t', true}, {"a\tb", ',', true}, // a tab is escaped everywhere
	} {
		if got := needsQuote(tc.s, tc.delim); got != tc.quote {
			t.Errorf("needsQuote(%q, %q) = %v, want %v", tc.s, tc.delim, got, tc.quote)
		}
	}
}

func TestAnUnknownDelimiterIsRefused(t *testing.T) {
	if _, err := EncodeWith(map[string]any{"a": 1}, Options{Delimiter: ';'}); err == nil {
		t.Fatal("';' was accepted as a delimiter")
	}
}

// Each header declares its own delimiter; nothing is inherited. A document
// mixing them, as a hand-edited or model-written one may, reads correctly.
func TestEachHeaderHasItsOwnDelimiter(t *testing.T) {
	text := "outer[2|]:\n  - inner[2\t]: x,y\tz\n  - plain[2]: p|q,r\n"
	got, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"outer": []any{
		map[string]any{"inner": []any{"x,y", "z"}},
		map[string]any{"plain": []any{"p|q", "r"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

// A keyless header at the top is the spec's root array. Models trained on the
// spec write it; Encode still writes a top-level list under "items".
func TestRootArrays(t *testing.T) {
	for text, want := range map[string]any{
		"[3]: a,b,c\n":                  []any{"a", "b", "c"},
		"[2]{id,name}:\n  1,x\n  2,y\n": []any{map[string]any{"id": int64(1), "name": "x"}, map[string]any{"id": int64(2), "name": "y"}},
		"[2|]{a|b}:\n  1|2\n  3|4\n":    []any{map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{"a": int64(3), "b": int64(4)}},
		"[2]:\n  - x\n  - y\n":          []any{"x", "y"},
		"[0]:\n":                        []any{},
	} {
		got, err := Parse(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v", text, got)
		}
	}
	var rows []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := Decode("[2]{id,name}:\n  1,x\n  2,y\n", &rows); err != nil || len(rows) != 2 || rows[1].Name != "y" {
		t.Fatalf("decode into a slice: %v %+v", err, rows)
	}
}

func TestRootArrayRefusals(t *testing.T) {
	for _, text := range []string{
		"[x]: 1\n",           // not a count
		"[2: a,b\n",          // no closing bracket
		"[2]{a,b}:\n  1\n",   // a row with too few values
		"[2]: a\nextra: 1\n", // the root array is the whole document
		"  [2]: a,b\n",       // a root header is not indented
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q parsed", text)
		}
	}
	if err := Strict("[3]: a,b\n", new(any)); err == nil {
		t.Error("Strict accepted a root array that miscounts")
	}
}

// The spec's canonical number form writes -0 as 0. The digits of every other
// float are unchanged: exactly what encoding/json writes.
func TestNegativeZeroIsCanonical(t *testing.T) {
	negZero := math.Copysign(0, -1)
	for _, v := range []any{
		map[string]any{"z": negZero},
		map[string]any{"z": float32(negZero)},
		map[string]any{"z": json.Number("-0")},
		struct {
			Z float64 `json:"z"`
		}{negZero},
	} {
		got, err := Encode(v)
		if err != nil {
			t.Fatal(err)
		}
		if got != "z: 0\n" {
			t.Errorf("%#v encoded as %q", v, got)
		}
	}
}

// A float JSON cannot carry has no TOON number either; in a generic tree it is
// written as quoted text rather than failing the whole document, as before.
func TestNonFiniteFloatsAreQuotedText(t *testing.T) {
	got, err := Encode(map[string]any{"a": math.NaN(), "b": math.Inf(1), "c": float32(math.Inf(-1))})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: \"NaN\"\nb: \"+Inf\"\nc: \"-Inf\"\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An `any` field that already holds a pointer is decoded THROUGH the pointer by
// encoding/json; the walk leaves that to it.
func TestDecodeIntoAnInterfaceHoldingAPointer(t *testing.T) {
	type dst struct {
		A any `json:"a"`
	}
	n1, n2 := 5, 5
	fast, slow := dst{A: &n1}, dst{A: &n2}
	ferr := Decode("a: 7\n", &fast)
	serr := decodeViaJSON("a: 7\n", &slow)
	if errText(ferr) != errText(serr) || !reflect.DeepEqual(fast, slow) {
		t.Fatalf("reflect %v %#v, json %v %#v", ferr, fast, serr, slow)
	}
}

// What the format is for, measured: the same 2,000 findings as TOON and as
// JSON. The README quotes these ratios; this fails if they stop being true.
func TestTOONIsSmallerThanJSON(t *testing.T) {
	r := benchStructs(2000)
	js, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	comma, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := EncodeWith(r, Options{Delimiter: '\t'})
	if err != nil {
		t.Fatal(err)
	}
	ratio := func(s string) float64 { return float64(len(s)) / float64(len(js)) }
	t.Logf("JSON %d bytes; TOON %d (%.0f%%), TOON tab %d (%.0f%%)",
		len(js), len(comma), 100*ratio(comma), len(tab), 100*ratio(tab))
	if ratio(comma) > 0.52 || ratio(tab) > 0.52 {
		t.Fatalf("TOON is no longer about half the size of JSON: comma %.2f, tab %.2f", ratio(comma), ratio(tab))
	}
	if len(tab) != len(comma)+1 {
		// Nothing in this table needs quoting under either delimiter, so the
		// only difference is the header declaring the tab: one byte. The tab
		// delimiter's saving is on prose.
		t.Fatalf("tab %d bytes, comma %d bytes", len(tab), len(comma))
	}
}

// Prose is where the delimiter matters: every cell holding a comma costs two
// quote characters under the comma delimiter and nothing under tab.
func TestTabDelimiterSavesTheQuotesOnProse(t *testing.T) {
	rows := make([]any, 200)
	for i := range rows {
		rows[i] = map[string]any{"id": i, "summary": "Checked the config, then restarted the worker"}
	}
	doc := map[string]any{"steps": rows}
	comma, _ := Encode(doc)
	tab, _ := EncodeWith(doc, Options{Delimiter: '\t'})
	// Two quotes saved per row, one byte spent declaring the tab in the header.
	if saved := len(comma) - len(tab); saved != 2*len(rows)-1 {
		t.Fatalf("tab saved %d bytes, want %d", saved, 2*len(rows)-1)
	}
	if strings.Contains(tab, `"`) {
		t.Fatalf("tab output still quotes something:\n%s", tab[:200])
	}
}

// The README says the struct and generic paths allocate far less than they did
// when every value went through encoding/json: 86,034 allocations to decode
// 2,000 rows into structs, 50,031 into a map, 33,829 to encode the structs.
// Allocation counts are deterministic where timings are not, so they are what
// is pinned; a change that sent ordinary values back through JSON would blow
// through every one of these budgets.
func TestAllocationBudgets(t *testing.T) {
	r := benchStructs(2000)
	text, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	generic, err := Encode(benchDoc(2000)) // the document BenchmarkDecode reads
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		budget float64
		run    func()
	}{
		{"decode into structs", 13000, func() { var out benchReport; _ = Decode(text, &out) }},
		{"decode into a map", 12500, func() { var out map[string]any; _ = Decode(generic, &out) }},
		{"encode structs", 13500, func() { _, _ = Encode(r) }},
	} {
		if got := testing.AllocsPerRun(5, tc.run); got > tc.budget {
			t.Errorf("%s: %.0f allocations, budget %.0f", tc.name, got, tc.budget)
		}
	}
}
