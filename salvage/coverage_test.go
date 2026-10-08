package salvage

import (
	"errors"
	"testing"
)

// Mismatched nesting, as in {"a": [}]}, is not truncation and not recoverable:
// the brackets contradict each other, so there is no honest reading of which
// one was meant. Scan abandons the region and reports it rather than returning
// whichever half happens to balance.
func TestScanMismatchedNesting(t *testing.T) {
	vals, truncated := Scan(`prefix {"a": [}]} suffix`)
	if len(vals) != 0 {
		t.Errorf("values = %q, want none", vals)
	}
	if !truncated {
		t.Error("truncated = false, want true")
	}
}

// A stray closing bracket in prose has nothing to close and is not an error.
func TestScanStrayCloser(t *testing.T) {
	vals, truncated := Scan(`a } b ] c {"ok":1}`)
	if len(vals) != 1 || vals[0].JSON != `{"ok":1}` {
		t.Fatalf("values = %+v, want the one object", vals)
	}
	if truncated {
		t.Error("truncated = true, want false: a stray closer is not truncation")
	}
}

// A string left open by a cut-off reply is truncation too: everything after it
// is inside a literal that never ends.
func TestScanUnterminatedString(t *testing.T) {
	if _, truncated := Scan(`{"a":1} then "oops`); !truncated {
		t.Error("truncated = false, want true")
	}
}

func TestScanOffsets(t *testing.T) {
	text := `xx {"a":1} yy`
	vals, _ := Scan(text)
	if len(vals) != 1 {
		t.Fatalf("values = %+v", vals)
	}
	if got := text[vals[0].Start:vals[0].End]; got != vals[0].JSON {
		t.Errorf("offsets slice %q, JSON is %q", got, vals[0].JSON)
	}
	if vals[0].Start != 3 {
		t.Errorf("Start = %d, want 3", vals[0].Start)
	}
}

// Escapes inside strings must not be read as structure: a \" does not end the
// string, so the brace after it is content.
func TestScanEscapedQuote(t *testing.T) {
	vals, truncated := Scan(`{"a":"he said \"{\" to me"}`)
	if len(vals) != 1 || truncated {
		t.Fatalf("values = %+v truncated = %v", vals, truncated)
	}
}

// DecodeOne is the fail-closed entry point: it refuses a truncated reply and
// refuses an ambiguous one, so a caller acting on the result never acts on a
// guess.
func TestDecodeOne(t *testing.T) {
	type verdict struct {
		Findings []string `json:"findings"`
	}

	t.Run("one value decodes", func(t *testing.T) {
		var v verdict
		if err := DecodeOne(`here it is: {"findings":["a","b"]}`, &v); err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(v.Findings) != 2 {
			t.Errorf("findings = %v", v.Findings)
		}
	})

	t.Run("truncated is refused", func(t *testing.T) {
		var v verdict
		// The exact shape that read as a clean review: the outer object never
		// closes, and the fragment inside it has no findings key at all.
		err := DecodeOne(`{"findings": [{"file": "a.go", "summary": "races"}`, &v)
		if !errors.Is(err, ErrTruncated) {
			t.Fatalf("err = %v, want ErrTruncated", err)
		}
		if v.Findings != nil {
			t.Errorf("destination was written despite the error: %v", v.Findings)
		}
	})

	t.Run("two answers are refused", func(t *testing.T) {
		var v verdict
		err := DecodeOne(`{"findings":["a"]} ... on reflection: {"findings":[]}`, &v)
		if !errors.Is(err, ErrAmbiguous) {
			t.Fatalf("err = %v, want ErrAmbiguous", err)
		}
	})

	t.Run("nothing decodes", func(t *testing.T) {
		var v verdict
		if err := DecodeOne("no json here at all", &v); !errors.Is(err, ErrNoJSON) {
			t.Errorf("err = %v, want ErrNoJSON", err)
		}
	})

	t.Run("needs a pointer", func(t *testing.T) {
		if err := DecodeOne(`{"findings":[]}`, verdict{}); err == nil {
			t.Error("a non-pointer destination was accepted")
		}
		var p *verdict
		if err := DecodeOne(`{"findings":[]}`, p); err == nil {
			t.Error("a nil pointer was accepted")
		}
	})
}

// Models asked for an object routinely return it wrapped in a list of one.
// Reading it should not require the caller to own a second type.
func TestDecodeOneUnwrapsASingletonArray(t *testing.T) {
	type doc struct {
		Name string `json:"name"`
	}
	var d doc
	if err := DecodeOne(`[{"name":"invoice"}]`, &d); err != nil {
		t.Fatalf("err = %v", err)
	}
	if d.Name != "invoice" {
		t.Errorf("name = %q", d.Name)
	}

	// Two elements is a list, and choosing from it is the caller's decision.
	var d2 doc
	if err := DecodeOne(`[{"name":"a"},{"name":"b"}]`, &d2); err == nil {
		t.Error("a two-element array was silently unwrapped")
	}

	// A list of one is still readable as a list.
	var list []doc
	if err := DecodeOne(`[{"name":"invoice"}]`, &list); err != nil {
		t.Fatalf("as a list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list = %v", list)
	}
}
