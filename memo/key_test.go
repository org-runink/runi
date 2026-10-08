package memo

import (
	"math"
	"testing"
)

func TestHashIsStableAcrossMapIterationOrder(t *testing.T) {
	// The reason this package has a Hash at all. Go randomises map iteration,
	// so hashing a map naively gives a different key every run and the cache
	// silently never hits.
	m := map[string]any{"z": 1, "a": "x", "m": true, "q": 3.5, "b": []string{"p", "q"}}
	first := Hash("model", m)
	for i := 0; i < 200; i++ {
		if got := Hash("model", m); got != first {
			t.Fatalf("hash changed between runs: %s then %s", first, got)
		}
	}
}

func TestHashSeparatesTypes(t *testing.T) {
	seen := map[string]string{}
	for _, c := range []struct {
		name string
		v    any
	}{
		{"string-1", "1"},
		{"int-1", 1},
		{"float-1", 1.0},
		{"bool-true", true},
		{"nil", nil},
		{"bytes-1", []byte("1")},
	} {
		h := Hash(c.v)
		if prev, dup := seen[h]; dup {
			t.Errorf("%s collides with %s", c.name, prev)
		}
		seen[h] = c.name
	}
}

func TestHashLengthPrefixPreventsConcatenationCollision(t *testing.T) {
	if Hash("ab", "c") == Hash("a", "bc") {
		t.Error(`Hash("ab","c") collides with Hash("a","bc")`)
	}
	if Hash([]string{"a", "b"}) == Hash([]string{"ab"}) {
		t.Error("slice element boundaries are not encoded")
	}
}

func TestHashFloatCanonicalisation(t *testing.T) {
	if Hash(math.NaN()) != Hash(math.NaN()) {
		t.Error("two NaNs must hash alike")
	}
	if Hash(0.0) != Hash(math.Copysign(0, -1)) {
		t.Error("-0.0 and 0.0 must share a key")
	}
	if Hash(1.0) == Hash(1.0000001) {
		t.Error("distinct floats must not collide")
	}
}

func TestHashNestedStructures(t *testing.T) {
	a := map[string]any{"outer": map[string]any{"i": 1, "j": 2}}
	b := map[string]any{"outer": map[string]any{"j": 2, "i": 1}}
	if Hash(a) != Hash(b) {
		t.Error("nested maps with the same content must hash alike")
	}
	c := map[string]any{"outer": map[string]any{"i": 1, "j": 3}}
	if Hash(a) == Hash(c) {
		t.Error("nested maps with different content must not collide")
	}
}

func TestHashUnsupportedTypeIsStableAndDistinct(t *testing.T) {
	type custom struct{ A int }
	h1, h2 := Hash(custom{1}), Hash(custom{1})
	if h1 != h2 {
		t.Error("an unsupported type must still hash stably")
	}
	if Hash(custom{1}) == Hash(custom{2}) {
		t.Error("different values of an unsupported type must differ")
	}
}

func TestNormalizeSpace(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  hello   world \n", "hello world"},
		{"a\t\tb\nc", "a b c"},
		{"", ""},
		{"   ", ""},
	} {
		if got := NormalizeSpace(c.in); got != c.want {
			t.Errorf("NormalizeSpace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if Hash(NormalizeSpace("a  b")) != Hash(NormalizeSpace("a\nb")) {
		t.Error("normalised whitespace should produce equal keys")
	}
}
