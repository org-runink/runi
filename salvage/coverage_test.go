package salvage

import "testing"

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
