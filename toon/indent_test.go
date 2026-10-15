package toon

import "testing"

// A document's indentation unit is its own, not an assumed two spaces.
//
// Every case here is the SAME tree written with a different unit or offset.
// Before the scanner inferred the unit, two spaces worked, three was refused
// outright, and four was the bad one: four is a multiple of two, so nothing
// looked wrong and the document was simply read at twice its true depth --
// every key nested under the wrong parent, and the caller got an empty map
// back with a nil error.
func TestIndentUnitIsInferredFromTheDocument(t *testing.T) {
	want := func(t *testing.T, got map[string]any) {
		t.Helper()
		inner, ok := got["outer"].(map[string]any)
		if !ok {
			t.Fatalf("outer is not a mapping: %#v (keys %v)", got["outer"], keysOf(got))
		}
		if inner["key"] != "value" {
			t.Errorf("outer.key = %#v, want \"value\"", inner["key"])
		}
		if got["after"] != "tail" {
			t.Errorf("after = %#v, want \"tail\"; it belongs at the root, not nested", got["after"])
		}
	}
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"two spaces", "outer:\n  key: value\nafter: tail\n"},
		{"three spaces", "outer:\n   key: value\nafter: tail\n"},
		{"four spaces", "outer:\n    key: value\nafter: tail\n"},
		{"eight spaces", "outer:\n        key: value\nafter: tail\n"},
		{"whole block shifted by two", "  outer:\n    key: value\n  after: tail\n"},
		{"shifted by three, unit of three", "   outer:\n      key: value\n   after: tail\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			if err := Decode(tc.doc, &got); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			want(t, got)
		})
	}
}

// A jump of two levels at once must not be mistaken for the unit. With
// indents of 0, 4 and 8 the smallest gap is 4 and so is the GCD; taking the
// largest or the last gap would read "deep" at depth 1 instead of 2.
func TestATwoLevelJumpDoesNotRedefineTheUnit(t *testing.T) {
	var got map[string]any
	if err := Decode("a:\n    b:\n        deep: 1\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	a, ok := got["a"].(map[string]any)
	if !ok {
		t.Fatalf("a is not a mapping: %#v", got["a"])
	}
	b, ok := a["b"].(map[string]any)
	if !ok {
		t.Fatalf("a.b is not a mapping: %#v", a["b"])
	}
	if b["deep"] != float64(1) && b["deep"] != 1 {
		t.Errorf("a.b.deep = %#v, want 1", b["deep"])
	}
}

// A document that never indents has no unit to find. It must still decode
// rather than divide by zero.
func TestAFlatDocumentNeedsNoUnit(t *testing.T) {
	var got map[string]any
	if err := Decode("a: 1\nb: 2\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2: %v", len(got), keysOf(got))
	}
}

// Tabs stay refused. Inferring the unit relaxes how many SPACES a level is,
// not what counts as indentation.
func TestATabIsStillRefused(t *testing.T) {
	var got map[string]any
	err := Decode("outer:\n\tkey: value\n", &got)
	if err == nil {
		t.Fatal("a tab-indented document decoded; it must be refused")
	}
	if se, ok := err.(*SyntaxError); !ok {
		t.Errorf("error is %T, want *SyntaxError: %v", err, se)
	}
}

func TestGCD(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{
		{0, 0, 0}, {0, 4, 4}, {4, 0, 4}, {2, 4, 2}, {4, 6, 2}, {3, 9, 3}, {7, 5, 1},
	} {
		if got := gcd(c.a, c.b); got != c.want {
			t.Errorf("gcd(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
