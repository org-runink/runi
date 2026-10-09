package toon

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeTheShapeTheFormatExistsFor(t *testing.T) {
	const in = `name: "a scalar"
tags[3]: red,green,blue
parts[2]{sku,qty}:
  A-1,4
  B-2,9
steps:
  - label: "first"
    done: true
  - label: "second"
    done: false
nested:
  depth: 2
  flag: null
`
	var got struct {
		Name  string   `json:"name"`
		Tags  []string `json:"tags"`
		Parts []struct {
			SKU string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"parts"`
		Steps []struct {
			Label string `json:"label"`
			Done  bool   `json:"done"`
		} `json:"steps"`
		Nested struct {
			Depth int `json:"depth"`
			Flag  *string
		} `json:"nested"`
	}
	if err := Decode(in, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "a scalar" {
		t.Errorf("name = %q", got.Name)
	}
	if !reflect.DeepEqual(got.Tags, []string{"red", "green", "blue"}) {
		t.Errorf("tags = %v", got.Tags)
	}
	if len(got.Parts) != 2 || got.Parts[0].SKU != "A-1" || got.Parts[1].Qty != 9 {
		t.Errorf("parts = %+v", got.Parts)
	}
	if len(got.Steps) != 2 || got.Steps[0].Label != "first" || got.Steps[1].Done {
		t.Errorf("steps = %+v", got.Steps)
	}
	if got.Nested.Depth != 2 || got.Nested.Flag != nil {
		t.Errorf("nested = %+v", got.Nested)
	}
}

// Models wrap replies in a fence about half the time, with or without a tag.
func TestDecodeAcceptsACodeFence(t *testing.T) {
	for _, in := range []string{
		"```toon\nk: 1\n```",
		"```\nk: 1\n```",
		"\n\n```toon\nk: 1\n```\n\n",
		"k: 1\n",
	} {
		var got map[string]any
		if err := Decode(in, &got); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got["k"] != float64(1) {
			t.Errorf("%q: k = %v", in, got["k"])
		}
	}
}

// A declared length that disagrees with the values is the model miscounting.
// The values are the data; the count restates it.
func TestDecodeTakesTheValuesOverTheCount(t *testing.T) {
	var got struct {
		F []string `json:"f"`
	}
	if err := Decode("f[5]: a,b,c\n", &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got.F, []string{"a", "b", "c"}) {
		t.Errorf("f = %v, want the three values that are there", got.F)
	}

	var strictGot struct {
		F []string `json:"f"`
	}
	err := Strict("f[5]: a,b,c\n", &strictGot)
	if err == nil {
		t.Fatal("Strict accepted a count that does not match")
	}
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 1 {
		t.Errorf("err = %v, want a SyntaxError on line 1", err)
	}
}

// A comma inside a quoted value is content, not a separator. Getting this
// wrong turns one sentence into two values and the count into a mismatch.
func TestQuotedCommasAreNotSeparators(t *testing.T) {
	var got struct {
		Sops []string `json:"sops"`
	}
	if err := Decode(`sops[2]: "Verify, then approve","Audit"`+"\n", &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Sops) != 2 || got.Sops[0] != "Verify, then approve" {
		t.Errorf("sops = %q", got.Sops)
	}
}

// An empty result is a result. A model that found nothing says so with a
// zero-length array, and that must not read as a failure.
func TestEmptyArraysAreNotFailures(t *testing.T) {
	for _, in := range []string{"rules[0]:\n", "objects[0]{label,material}:\n"} {
		var got map[string]any
		if err := Decode(in, &got); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		for _, v := range got {
			arr, ok := v.([]any)
			if !ok || len(arr) != 0 {
				t.Errorf("%q: got %#v, want an empty array", in, v)
			}
		}
	}
}

func TestScalarTypes(t *testing.T) {
	var got map[string]any
	in := "i: 42\nf: 1.5\nt: true\nf2: false\nn: null\nbare: hello\nempty:\nneg: -7\n"
	if err := Decode(in, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"i": float64(42), "f": 1.5, "t": true, "f2": false,
		"n": nil, "bare": "hello", "empty": nil, "neg": float64(-7),
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
}

func TestEscapes(t *testing.T) {
	var got map[string]any
	in := `s: "a\"b\\c\nd\te\u0041"` + "\n"
	if err := Decode(in, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "a\"b\\c\nd\teA"; got["s"] != want {
		t.Errorf("s = %q, want %q", got["s"], want)
	}
}

// Structure is never guessed at. Each of these is a model producing something
// that cannot be read two ways without inventing an answer.
func TestStructuralErrorsAreRefused(t *testing.T) {
	cases := []struct{ name, in string }{
		{"row with the wrong column count", "p[2]{a,b}:\n  1,2,3\n"},
		{"indent that matches no block", "a: 1\n    b: 2\n"},
		{"odd indent", "a:\n   b: 2\n"},
		{"tab indent", "a:\n\tb: 2\n"},
		{"unterminated string", "s: \"never closed\n"},
		{"no colon", "just some prose\n"},
		{"array length is not a number", "a[x]: 1\n"},
		{"array declaration missing bracket", "a[2: 1\n"},
		{"tabular with no fields", "a[1]{}:\n  1\n"},
		{"bad escape", `s: "a\qb"` + "\n"},
		{"trailing backslash", `s: "a\"` + "\n"},
		{"short unicode escape", `s: "a\u12"` + "\n"},
		{"duplicate key", "a: 1\na: 2\n"},
		{"empty key", ": 1\n"},
		{"list where a key belongs", "a: 1\n- 2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got map[string]any
			err := Decode(c.in, &got)
			if err == nil {
				t.Fatalf("accepted %q", c.in)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want a SyntaxError", err)
			}
			if se.Line < 1 {
				t.Errorf("line = %d, want a real line number", se.Line)
			}
			if !strings.Contains(se.Error(), "toon: line") {
				t.Errorf("message does not say where: %q", se.Error())
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	type part struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}
	type doc struct {
		Name  string   `json:"name"`
		Tags  []string `json:"tags"`
		Parts []part   `json:"parts"`
		Meta  struct {
			Depth int  `json:"depth"`
			OK    bool `json:"ok"`
		} `json:"meta"`
	}
	in := doc{Name: "with, a comma", Tags: []string{"a", "b"}, Parts: []part{{"A-1", 4}, {"B-2", 9}}}
	in.Meta.Depth = 3
	in.Meta.OK = true

	text, err := Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back doc
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if !reflect.DeepEqual(in, back) {
		t.Errorf("round trip changed the value:\n%s\nin   = %+v\nback = %+v", text, in, back)
	}
	// The tabular form is what makes TOON worth using: field names once.
	if !strings.Contains(text, "parts[2]{qty,sku}:") {
		t.Errorf("uniform objects were not written as rows:\n%s", text)
	}
}

func TestEncodeQuotesWhatWouldReadBackWrong(t *testing.T) {
	for _, s := range []string{"", "true", "false", "null", "42", "1.5", " padded ", "a,b", `a"b`, "a:b", "a\nb", "- dash"} {
		text, err := Encode(map[string]string{"k": s})
		if err != nil {
			t.Fatalf("encode %q: %v", s, err)
		}
		var back map[string]string
		if err := Decode(text, &back); err != nil {
			t.Fatalf("decode %q (from %q): %v", text, s, err)
		}
		if back["k"] != s {
			t.Errorf("%q round-tripped as %q via %q", s, back["k"], strings.TrimSpace(text))
		}
	}
}

func TestEncodeRefusesABareScalar(t *testing.T) {
	if _, err := Encode(42); err == nil {
		t.Error("encoded a bare scalar as a document")
	}
	if _, err := Encode(make(chan int)); err == nil {
		t.Error("encoded an unmarshalable value")
	}
}

func TestParseWithoutAType(t *testing.T) {
	got, err := Parse("a: 1\nb[2]: x,y\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want a map", got)
	}
	if m["a"] != int64(1) {
		t.Errorf("a = %#v", m["a"])
	}
	if l, ok := m["b"].([]any); !ok || len(l) != 2 {
		t.Errorf("b = %#v", m["b"])
	}
}

func TestEmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n\n", "```\n```"} {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if m, ok := got.(map[string]any); !ok || len(m) != 0 {
			t.Errorf("%q: got %#v, want an empty map", in, got)
		}
	}
}

func TestDecodeNeedsAPointer(t *testing.T) {
	if err := Decode("a: 1\n", map[string]any{}); err == nil {
		t.Error("accepted a non-pointer destination")
	}
}

// Objects that do not share a shape cannot be rows, so they are written as a
// dash list instead. This is the path a real model reply takes most often,
// because optional fields are normal.
func TestEncodeNonUniformObjectsAsADashList(t *testing.T) {
	in := map[string]any{
		"steps": []any{
			map[string]any{"label": "first", "done": true},
			map[string]any{"label": "second"},
		},
	}
	text, err := Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(text, "{") {
		t.Errorf("mixed shapes were written as rows:\n%s", text)
	}
	if !strings.Contains(text, "- ") {
		t.Errorf("expected a dash list:\n%s", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	steps, ok := back["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("steps = %#v\nfrom:\n%s", back["steps"], text)
	}
	first := steps[0].(map[string]any)
	if first["label"] != "first" || first["done"] != true {
		t.Errorf("first = %#v\nfrom:\n%s", first, text)
	}
	if second := steps[1].(map[string]any); second["label"] != "second" {
		t.Errorf("second = %#v\nfrom:\n%s", second, text)
	}
}

// A list whose items are themselves lists or objects-with-objects has no row
// form either, and must still survive the trip.
func TestEncodeNestedShapes(t *testing.T) {
	in := map[string]any{
		"outer": []any{
			map[string]any{"inner": map[string]any{"deep": "value"}},
			map[string]any{"inner": map[string]any{"deep": "other"}},
		},
	}
	text, err := Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	outer := back["outer"].([]any)
	if len(outer) != 2 {
		t.Fatalf("outer = %#v\nfrom:\n%s", outer, text)
	}
	got := outer[0].(map[string]any)["inner"].(map[string]any)["deep"]
	if got != "value" {
		t.Errorf("deep = %v\nfrom:\n%s", got, text)
	}
}

func TestEncodeEmptyShapes(t *testing.T) {
	text, err := Encode(map[string]any{
		"none":  []any{},
		"blank": map[string]any{},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(text, "none[0]:") {
		t.Errorf("empty array not declared:\n%s", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if arr, ok := back["none"].([]any); !ok || len(arr) != 0 {
		t.Errorf("none = %#v", back["none"])
	}
}

// A document may be a list at the top level, not only a mapping.
func TestEncodeTopLevelList(t *testing.T) {
	text, err := Encode([]any{
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 3, "b": 4},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(text, "items[2]{a,b}:") {
		t.Errorf("top-level list not named or not tabular:\n%s", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if rows, ok := back["items"].([]any); !ok || len(rows) != 2 {
		t.Errorf("items = %#v", back["items"])
	}
}

func TestEncodeScalarListsInline(t *testing.T) {
	text, err := Encode(map[string]any{"tags": []any{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(text, "tags[3]: a,b,c") {
		t.Errorf("scalar list not inline:\n%s", text)
	}
}

// A list of empty objects has no fields to make rows from.
func TestEncodeListOfEmptyObjects(t *testing.T) {
	text, err := Encode(map[string]any{"xs": []any{map[string]any{}, map[string]any{}}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(text, "{") {
		t.Errorf("empty objects were written as rows:\n%s", text)
	}
}

// A top-level dash list decodes without a key.
func TestDecodeTopLevelDashList(t *testing.T) {
	got, err := Parse("- 1\n- 2\n- three\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	l, ok := got.([]any)
	if !ok || len(l) != 3 || l[2] != "three" {
		t.Fatalf("got %#v", got)
	}
}

// A dash item with nothing after it opens a block on the following lines.
func TestDecodeDashItemWithIndentedBlock(t *testing.T) {
	got, err := Parse("xs:\n  -\n    a: 1\n  -\n    a: 2\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	xs := got.(map[string]any)["xs"].([]any)
	if len(xs) != 2 || xs[0].(map[string]any)["a"] != int64(1) {
		t.Fatalf("xs = %#v", xs)
	}
}

// An array whose body is a mapping rather than list items is not an array.
func TestArrayBodyMustBeListItems(t *testing.T) {
	var got map[string]any
	if err := Decode("xs[1]:\n  a: 1\n", &got); err == nil {
		t.Error("accepted a mapping as an array body")
	}
}

// A list holding both scalars and objects has no row form and no inline form.
func TestEncodeMixedList(t *testing.T) {
	text, err := Encode(map[string]any{"xs": []any{"plain", map[string]any{"k": "v"}}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(text, "- plain") {
		t.Errorf("scalar item not written with a dash:\n%s", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	xs := back["xs"].([]any)
	if len(xs) != 2 || xs[0] != "plain" {
		t.Errorf("xs = %#v\nfrom:\n%s", xs, text)
	}
}

// Rows need every item to carry every field. One missing value would shift
// every later cell into the wrong column.
func TestEncodeRejectsRowsWithAMissingField(t *testing.T) {
	text, err := Encode(map[string]any{"xs": []any{
		map[string]any{"a": 1, "b": 2},
		map[string]any{"a": 3, "c": 4}, // same count, different names
	}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(text, "{") {
		t.Errorf("items with different fields were written as rows:\n%s", text)
	}
}

func TestEncodeNullAndBackslash(t *testing.T) {
	text, err := Encode(map[string]any{"n": nil, "s": `back\slash`})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(text, "n: null") {
		t.Errorf("nil not written as null:\n%s", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if back["n"] != nil || back["s"] != `back\slash` {
		t.Errorf("round trip: %#v\nfrom:\n%s", back, text)
	}
}

// The conversion back from marshalled JSON cannot fail, and the refusal is
// still exercised: a value that could not be re-read must not be emitted as a
// document with a silently missing half.
func TestEncodeReportsAFailedConversion(t *testing.T) {
	orig := jsonToValue
	jsonToValue = func([]byte) (any, error) { return nil, errors.New("cannot re-read") }
	defer func() { jsonToValue = orig }()

	if _, err := Encode(map[string]any{"a": 1}); err == nil {
		t.Error("encoded a document whose conversion failed")
	}
}

// A list of lists has neither a row form nor an inline form.
func TestEncodeListOfLists(t *testing.T) {
	text, err := Encode(map[string]any{"grid": []any{[]any{1, 2}, []any{3, 4}}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Logf("list-of-lists renders as:\n%s", text)
		t.Fatalf("decode: %v", err)
	}
}

// A list inside a list must survive as a list. It used to be written as Go's
// %v of the slice inside quotes, so [[1,2],[3,4]] decoded back as the two
// STRINGS "[1 2]" and "[3 4]" -- the structure replaced by a description of
// it. The previous test for this asserted only that the result decoded, which
// it did, wrongly.
func TestNestedListsSurviveEncoding(t *testing.T) {
	in := map[string]any{"grid": []any{
		[]any{json.Number("1"), json.Number("2")},
		[]any{json.Number("3"), json.Number("4")},
	}}
	text, err := Encode(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	rows, ok := back["grid"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("grid came back as %T %v\n%s", back["grid"], back["grid"], text)
	}
	for i, row := range rows {
		cells, ok := row.([]any)
		if !ok {
			t.Fatalf("row %d came back as %T (%v), not a list\n%s", i, row, row, text)
		}
		if len(cells) != 2 {
			t.Fatalf("row %d has %d cells\n%s", i, len(cells), text)
		}
	}
}

// An empty list directly inside a list is refused rather than mangled: a dash
// with nothing after it reads back as an empty object, so [[]] would silently
// become [{}].
func TestAnEmptyListInsideAListIsRefused(t *testing.T) {
	if _, err := Encode(map[string]any{"a": []any{[]any{}}}); err == nil {
		t.Fatal("an empty nested list was encoded")
	}
	// One level up it is fine: an empty list as a field's value round-trips.
	text, err := Encode(map[string]any{"a": []any{}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, ok := back["a"].([]any); !ok || len(got) != 0 {
		t.Fatalf("empty list came back as %T %v", back["a"], back["a"])
	}
}

// An empty object as a field's value reads back as null, and that is pinned
// here rather than fixed. The encoder writes it as a bare "key:", and the
// parser cannot tell that from a key whose value a model simply left blank.
// Deciding it means "empty object" would make Decode stricter on exactly the
// input this package exists to be lenient about -- a model's half-finished
// line -- and the difference between a nil map and an empty one is not one a
// caller acts on. The asymmetry is documented instead of being silently
// tolerated by the round-trip property.
func TestAnEmptyObjectReadsBackAsNull(t *testing.T) {
	text, err := Encode(map[string]any{"a": map[string]any{}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if text != "a:\n" {
		t.Fatalf("encoded as %q", text)
	}
	var back map[string]any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back["a"] != nil {
		t.Fatalf("expected null, got %T %v", back["a"], back["a"])
	}
}

// Parse keeps an integer as an int64, so every digit survives. Decode fills
// its destination through encoding/json, which parses a number into a float64
// unless the destination says otherwise. For an integer past
// 2^53 that loses digits, which for a model reply carrying an ID is a wrong
// answer rather than a rounding. Parse keeps the number as json.Number, so it
// is the entry point for untyped data. Decoding into a typed destination with
// an int64 field is exact as well; it is only `any` that rounds.
func TestLargeIntegersNeedParseNotDecode(t *testing.T) {
	const big = "9007199254740993" // 2^53 + 1, the first integer a float64 cannot hold
	text := "id: " + big + "\n"

	// Parse keeps every digit.
	doc, err := Parse(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, ok := doc.(map[string]any)["id"].(int64)
	if !ok {
		t.Fatalf("Parse gave %T, want int64", doc.(map[string]any)["id"])
	}
	if fmt.Sprint(got) != big {
		t.Errorf("Parse returned %d, want %s", got, big)
	}

	// A typed destination is exact too.
	var typed struct {
		ID int64 `json:"id"`
	}
	if err := Decode(text, &typed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fmt.Sprint(typed.ID) != big {
		t.Errorf("int64 field got %d, want %s", typed.ID, big)
	}

	// Into `any`, encoding/json rounds. Pinned so the limit is known rather
	// than discovered.
	var loose map[string]any
	if err := Decode(text, &loose); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f, ok := loose["id"].(float64); !ok || fmt.Sprintf("%.0f", f) == big {
		t.Errorf("expected a rounded float64, got %T %v", loose["id"], loose["id"])
	}
}

// Encode of a top-level LIST gives it the key "items", because a TOON document
// is a mapping and a bare list has no key to hang on. It therefore does not
// round-trip to a list: it decodes as an object with one field. Pinned here so
// the asymmetry is a documented property of the format rather than a surprise
// found in production.
func TestATopLevelListGainsAnItemsKey(t *testing.T) {
	text, err := Encode([]any{map[string]any{"id": json.Number("1")}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back any
	if err := Decode(text, &back); err != nil {
		t.Fatalf("decode: %v\n%s", err, text)
	}
	m, ok := back.(map[string]any)
	if !ok {
		t.Fatalf("a top-level list decoded as %T, want map", back)
	}
	if _, ok := m["items"]; !ok {
		t.Fatalf("want an \"items\" key, got %v", m)
	}
}

// The branches below are either unreachable through the public API or awkward
// to reach through it, so they are exercised where they live rather than left
// uncovered or chased with a test that asserts nothing.

func TestUnreachableGuards(t *testing.T) {
	// formatScalar's default arm. Composites no longer reach it -- a nested
	// list is written as a nested list and a map as a mapping -- but it stays
	// as the backstop for a type a future change might let through, and it
	// must quote rather than emit something that reads back as structure.
	if got := formatScalar(struct{ A int }{1}); got == "" || got[0] != '"' {
		t.Errorf("formatScalar of an unexpected type gave %q, want a quoted string", got)
	}

	// jsonToValue's error cannot happen: the bytes came from json.Marshal one
	// line earlier.
	orig := jsonToValue
	defer func() { jsonToValue = orig }()
	jsonToValue = func([]byte) (any, error) { return nil, errors.New("boom") }
	if _, err := Encode(map[string]any{"a": 1}); err == nil {
		t.Error("Encode ignored a jsonToValue failure")
	}
}

func TestMarshalDocFailureIsReported(t *testing.T) {
	// marshalDoc's error cannot happen either: the document is maps, slices
	// and scalars built by this package.
	orig := marshalDoc
	defer func() { marshalDoc = orig }()
	marshalDoc = func(any) ([]byte, error) { return nil, errors.New("boom") }
	var got map[string]any
	if err := Decode("a: 1\n", &got); err == nil {
		t.Error("Decode ignored a marshal failure")
	}
}

// Structural mistakes a model actually makes: a list item indented past its
// siblings, a key repeated inside one item, a block indented two levels at
// once. Each must be refused with a line number, never guessed at.
func TestMoreStructuralRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"dash item indented past its list", "a[2]:\n  - 1\n      - 2\n"},
		{"duplicate key inside a dash item", "a[1]:\n  - x: 1\n    x: 2\n"},
		{"value block indented two levels", "a:\n      b: 1\n"},
		{"array body indented two levels", "a[1]:\n      - 1\n"},
	} {
		var got any
		err := Decode(tc.text, &got)
		if err == nil {
			t.Errorf("%s: accepted %q", tc.name, tc.text)
			continue
		}
		if _, ok := err.(*SyntaxError); !ok {
			t.Errorf("%s: got %T (%v), want *SyntaxError", tc.name, err, err)
		}
	}
}

// Every escape the format accepts, including the two that have no printable
// form, and the one that is malformed.
func TestEveryEscape(t *testing.T) {
	var got map[string]any
	if err := Decode("s: \"a\\bb\\fc\\u0041d\\/e\"\n", &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "a\bb\fcAd/e"; got["s"] != want {
		t.Errorf("got %q, want %q", got["s"], want)
	}
	for _, bad := range []string{
		"s: \"\\uZZZZ\"\n",
		"s: \"\\u00\"\n",
		"s: \"\\q\"\n",
		"s: \"abc\\\"\n",
	} {
		var v map[string]any
		if err := Decode(bad, &v); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

// A declared count with no body at all. The count is the one piece of
// redundancy TOON has, and a reply that promises two items and delivers none
// is exactly the truncation it exists to catch -- but only under Strict.
// Decode takes the values over the count on purpose, because a model that
// miscounts its own list is the common case and the values are the answer.
func TestADeclaredCountWithNoBodyIsRefusedOnlyByStrict(t *testing.T) {
	var lenient any
	if err := Decode("a[2]:\n", &lenient); err != nil {
		t.Fatalf("Decode should take the values over the count: %v", err)
	}

	var got any
	err := Strict("a[2]:\n", &got)
	if err == nil {
		t.Fatal("Strict accepted a[2] with no items")
	}
	if _, ok := err.(*SyntaxError); !ok {
		t.Fatalf("got %T (%v), want *SyntaxError", err, err)
	}
}

// jsonToValue's own error arm. Swapping the variable covers the caller's
// branch but never runs the real function, so it is called here with the
// input it exists to reject -- which Encode cannot hand it, because those
// bytes always come from json.Marshal.
func TestJSONToValueRejectsBrokenInput(t *testing.T) {
	if _, err := jsonToValue([]byte("{")); err == nil {
		t.Error("jsonToValue accepted a truncated object")
	}
}
