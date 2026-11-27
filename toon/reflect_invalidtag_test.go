package toon

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A json tag whose name encoding/json's own grammar rejects (here a
// backslash) is not something this package may interpret on its own: the
// toolchain decides. Go 1.27 builds encoding/json on its v2 core by default
// and reads such a name differently from the v1 rule, so the only answer that
// agrees with encoding/json on every toolchain is to hand the type to it.
type invalidTagEnc struct {
	F0 int    `json:"bad\\name"`
	F1 string `json:"ok"`
}

type invalidTagDec struct {
	F0 []int32           `json:",omitempty"`
	F1 []map[string]bool `json:"bad\\name"`
	F2 []int32
}

func TestInvalidJSONTagNameFollowsEncodingJSON(t *testing.T) {
	in := invalidTagEnc{F0: 7, F1: "x"}
	got, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var viaJSON any
	if err := json.Unmarshal(b, &viaJSON); err != nil {
		t.Fatal(err)
	}
	want, err := Encode(viaJSON)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("encode disagrees with encoding/json\n--- reflect ---\n%s\n--- json ---\n%s", got, want)
	}
}

func TestInvalidJSONTagNameDecodeFollowsEncodingJSON(t *testing.T) {
	text := "F1: null\nF2[1]: -412067132\n"
	var viaToon invalidTagDec
	if err := Decode(text, &viaToon); err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := Decode(text, &doc); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var viaJSON invalidTagDec
	if err := json.Unmarshal(b, &viaJSON); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viaToon, viaJSON) {
		t.Fatalf("decode disagrees with encoding/json\nreflect: %#v\njson:    %#v", viaToon, viaJSON)
	}
}
