package toon

import "testing"

// A struct tagged for TOON and nothing else maps by its DECLARED name.
//
// Before the toon tag was consulted, these decoded by Go field name: the
// document says "current_thought", the struct looked for "CurrentThought",
// the field stayed zero and no error was raised. The failure was invisible to
// a round-trip test through this package alone, because encoding chose the
// same wrong name and the pair agreed with each other.
type toonOnly struct {
	CurrentThought string         `toon:"current_thought"`
	HasAction      bool           `toon:"has_action"`
	FinalResult    map[string]any `toon:"final_result_json"`
}

func TestAToonTagMapsWhenThereIsNoJSONTag(t *testing.T) {
	const doc = "current_thought: thinking\nhas_action: true\nfinal_result_json:\n  k: v\n"
	var got toonOnly
	if err := Decode(doc, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.CurrentThought != "thinking" {
		t.Errorf("CurrentThought = %q, want \"thinking\"", got.CurrentThought)
	}
	if !got.HasAction {
		t.Error("HasAction = false, want true")
	}
	if got.FinalResult["k"] != "v" {
		t.Errorf("FinalResult = %#v, want {k:v}", got.FinalResult)
	}
}

// Encoding uses the same mapping, so a toon-tagged struct round-trips through
// its declared names rather than through Go's.
func TestAToonTaggedStructRoundTrips(t *testing.T) {
	in := toonOnly{CurrentThought: "x", HasAction: true}
	text, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var back toonOnly
	if err := Decode(text, &back); err != nil {
		t.Fatalf("Decode: %v\n%s", err, text)
	}
	if back.CurrentThought != "x" || !back.HasAction {
		t.Errorf("round trip lost data: %#v from %q", back, text)
	}
}

// json WINS when both are present. A struct that already round-trips as JSON
// must be unaffected by this, which is the contract the package states.
func TestAJSONTagWinsOverAToonTag(t *testing.T) {
	type both struct {
		V string `toon:"from_toon" json:"from_json"`
	}
	var byJSON both
	if err := Decode("from_json: yes\n", &byJSON); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if byJSON.V != "yes" {
		t.Errorf("V = %q; the json tag must win", byJSON.V)
	}
	// A SEPARATE destination. Reusing the one above left V set to "yes" from
	// the decode that was supposed to succeed, so this check read its own
	// earlier result as proof the toon name had been accepted and failed on a
	// correct implementation.
	var byToon both
	if err := Decode("from_toon: yes\n", &byToon); err == nil && byToon.V == "yes" {
		t.Error("the toon name was accepted although a json tag is present")
	}
}

// An EMPTY json tag is deliberate and must not fall through to toon. This is
// why the lookup distinguishes an absent tag from an empty one.
func TestAnEmptyJSONTagDoesNotFallThroughToToon(t *testing.T) {
	type empty struct {
		V string `toon:"from_toon" json:""`
	}
	var got empty
	// With json:"" the name is invalid, so the field maps by its Go name.
	if err := Decode("V: yes\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.V != "yes" {
		t.Errorf("V = %q, want \"yes\" — an empty json tag maps by field name", got.V)
	}
}

// `toon:"-"` skips the field, exactly as `json:"-"` does.
func TestAToonDashSkipsTheField(t *testing.T) {
	type skipped struct {
		Kept    string `toon:"kept"`
		Ignored string `toon:"-"`
	}
	var got skipped
	if err := Decode("kept: a\nIgnored: b\n", &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Kept != "a" {
		t.Errorf("Kept = %q, want \"a\"", got.Kept)
	}
	if got.Ignored != "" {
		t.Errorf("Ignored = %q, want empty — toon:\"-\" must skip", got.Ignored)
	}
}

// Options still parse from a toon tag: it is the same grammar as json's.
func TestToonTagOptionsParse(t *testing.T) {
	type opt struct {
		V string `toon:"v,omitempty"`
	}
	text, err := Encode(opt{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if text != "" && text != "\n" {
		var back opt
		if err := Decode(text, &back); err != nil {
			t.Fatalf("Decode %q: %v", text, err)
		}
	}
}
