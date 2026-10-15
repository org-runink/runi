package toon

import (
	"errors"
	"testing"
)

// A document's indentation unit is its own, not an assumed two spaces.
//
// Every case here is the SAME tree written with a different unit or offset.
// A model that indents consistently with three or four spaces has described
// exactly one tree and reading it requires inventing nothing, which is the
// standard this package holds elsewhere; two spaces was simply the unit the
// scanner happened to assume.
func TestIndentUnitIsTheDocumentsOwn(t *testing.T) {
	check := func(t *testing.T, doc string) {
		t.Helper()
		var got map[string]any
		if err := Decode(doc, &got); err != nil {
			t.Fatalf("Decode(%q): %v", doc, err)
		}
		inner, ok := got["outer"].(map[string]any)
		if !ok {
			t.Fatalf("outer is not a mapping: %#v", got["outer"])
		}
		if inner["key"] != "value" {
			t.Errorf("outer.key = %#v, want \"value\"", inner["key"])
		}
		if got["after"] != "tail" {
			t.Errorf("after = %#v, want \"tail\" at the root", got["after"])
		}
	}
	for _, tc := range []struct{ name, doc string }{
		{"two spaces", "outer:\n  key: value\nafter: tail\n"},
		{"three spaces", "outer:\n   key: value\nafter: tail\n"},
		{"four spaces", "outer:\n    key: value\nafter: tail\n"},
		{"eight spaces", "outer:\n        key: value\nafter: tail\n"},
		{"whole block shifted", "  outer:\n    key: value\n  after: tail\n"},
		{"shifted by three, unit of three", "   outer:\n      key: value\n   after: tail\n"},
	} {
		t.Run(tc.name, func(t *testing.T) { check(t, tc.doc) })
	}

	t.Run("whole_block_shifted_root_array", func(t *testing.T) {
		got, err := Parse("  [2]: a,b\n")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		// Parse returns a root array as a bare slice. Accept the mapping
		// form too rather than pinning the shape here: this test is about
		// the SHIFT being normalised, and asserting the container as well
		// would make it fail for an unrelated reason if that ever changed.
		items, ok := got.([]any)
		if !ok {
			if m, isMap := got.(map[string]any); isMap {
				items, ok = m["items"].([]any)
			}
		}
		if !ok || len(items) != 2 {
			t.Fatalf("got %#v (%T), want a two-item root array", got, got)
		}
	})
}

// Widening the unit does NOT widen consistency. The unit is the first step
// the document makes; every later indent must be a multiple of it.
//
// This is the property a greatest-common-divisor would have destroyed. A GCD
// divides every indent by construction, so it can never refuse one -- it
// would turn "any consistent indent" into "any indent", which is a different
// promise and not the one being made.
func TestAnInconsistentIndentIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"steps 4 then 6", "a:\n    b:\n      c: 1\n"},
		{"steps 2 then 5", "a:\n  b:\n     c: 1\n"},
		{"sibling off the unit", "a:\n    b: 1\n      c: 2\nd: 3\n"},
		// A later line SHALLOWER than the first. The zero is the shallowest
		// line in the document, not the first one -- which matters because a
		// whole-block shift is recognised by every line sharing an offset,
		// and the first line is only the outermost when that holds. Here it
		// does not: indents are 4, 2, 7, so the zero is 2, the unit is the
		// first step (2), and 5 is not a multiple of it.
		{"first line deeper than a later one", "    a: 1\n  b: 2\n       c: 3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got any
			err := Decode(tc.doc, &got)
			if err == nil {
				t.Fatalf("accepted %q", tc.doc)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Errorf("got %T (%v), want *SyntaxError", err, err)
			}
		})
	}
}

// A jump of two levels is read as two levels, because the unit was already
// fixed by the first step. Taking the unit from the deepest line instead
// would read "deep" at depth 1.
func TestATwoLevelJumpKeepsTheEstablishedUnit(t *testing.T) {
	var got map[string]any
	if err := Decode("a:\n    b:\n        deep: 1\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	b, ok := got["a"].(map[string]any)["b"].(map[string]any)
	if !ok {
		t.Fatalf("a.b is not a mapping: %#v", got["a"])
	}
	if b["deep"] != float64(1) && b["deep"] != 1 {
		t.Errorf("a.b.deep = %#v, want 1", b["deep"])
	}
}

// A document that never indents has no unit to find, and must decode rather
// than divide by zero.
func TestAFlatDocumentNeedsNoUnit(t *testing.T) {
	var got map[string]any
	if err := Decode("a: 1\nb: 2\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2", len(got))
	}
}

// Tabs stay refused: this relaxes how many SPACES make a level, not what
// counts as indentation.
func TestATabIsStillRefused(t *testing.T) {
	var got map[string]any
	err := Decode("outer:\n\tkey: value\n", &got)
	if err == nil {
		t.Fatal("a tab-indented document decoded; it must be refused")
	}
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Errorf("got %T, want *SyntaxError", err)
	}
}
