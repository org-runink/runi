package memo

import (
	"encoding"
	"errors"
	"math"
	"testing"
	"time"
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

// Hash promises that keys built from the types it lists do not change within
// v1: a key persisted, logged or shared between processes stays valid across
// upgrades. These digests were recorded from the implementation and must
// never be regenerated to make this test pass.
func TestHashIsStableForSupportedTypes(t *testing.T) {
	inputs := [][]any{
		{},
		{nil},
		{""},
		{"model-v3"},
		{[]byte(nil)},
		{[]byte("raw")},
		{true},
		{false},
		{int(-7)}, {int8(-7)}, {int16(300)}, {int32(-70000)}, {int64(math.MinInt64)},
		{uint(7)}, {uint8(255)}, {uint16(65535)}, {uint32(1 << 31)}, {uint64(math.MaxUint64)},
		{float32(0.25)}, {1.5}, {math.NaN()}, {math.Copysign(0, -1)}, {math.Inf(1)},
		{[]string(nil)}, {[]string{"a", "b"}},
		{[]any{1, "x", nil, 2.5, []any{true}}},
		{map[string]any(nil)}, {map[string]any{"q": "hi", "lang": "en", "n": map[string]any{"z": 1, "a": []string{"k"}}}},
		{map[string]string{"b": "2", "a": "1"}},
		{"model-v3", 0.2, map[string]any{"q": "x", "lang": "en"}},
	}
	want := []string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"594e519ae499312b29433b7dd8a97ff068defcba9755b6d5d00e84c524d67b06",
		"76ca65fa532efea2d73e4cb2775f96cfb650d5c8c9dbb78594a01f4cf8bb3bfe",
		"ab7f323979b5d2addc8640b0ff2669efca7152364290bd8f5ca1803c13b6e584",
		"899b80c8dc11d5c2a65d67a4c3b6f3bebd2a1e990a9f79ac3d3c380533bb7c08",
		"26fce1762e210e5a7a6fabbff6fc8b4d8377442b67893381e9c327c5d915cb91",
		"3bae75222314d7290e861d7e9b41842ddc44b60cf9fbc081fcaf33a27187bf92",
		"66b20ed1acee9ce0999d8c227f9869225442e2915aa3a47ab7102d916ef8c1c2",
		"c58d36fa34bf57e933a9e878c70c47a326b0120c4332b05d40785e216bab0788",
		"c58d36fa34bf57e933a9e878c70c47a326b0120c4332b05d40785e216bab0788",
		"18f6f760aca97c971ff218c4c60c409da0b57a0d326e6913f971dbad54d6b02b",
		"34ed9748aa4eb545a656fcd0557c750a4d85b91ad62cf2e49687e112904a492d",
		"04a0d2ad4df0a681a9aa7a191d5d4e1e2e20b788c512ef7978012b608020bf06",
		"2e81eaf39e2edb802c23011c4035010a3380678530a62831b909b782d1391cea",
		"abb2ea6cef62e7052b1ca388f0d673d4988f2480433bd40cd0e03490f9c5ef73",
		"b0f9f9adf398f2c3dec5c09651147aa2cc7945fe6e277b2067b893cbbf7bb199",
		"ad4e500d0d4373b17afbeb1b33ee6743fc0a00a4fa84752fe2c5ec8a868fee32",
		"0892fe8cef0e4ab3c14087d9495d35601261007e22694a936d8478b8174de816",
		"3cc6c34e17ba756aadb756dce38ba5640875eab637f37b9a0e9261cd0f8069af",
		"a96d01ba75a32a1a4602ce3c8727baa494f148bf0522a36be00de6d672eb35f6",
		"a115379df1d4d555f1e17049acc7a787f9f114c720c7262ad31acac8b8148fbd",
		"f655b54c1c587ebf4312989497fd01048213bceaf8ebd51c9dd10319c4c2cdf8",
		"832ac8f24fc63c762029e0f66677653a270207ff4163a75b4c590ec76ecf1017",
		"9a87cc3076dc0da4df27eb28fb2a51793333aff1d678ed34602edebbedf61d0e",
		"6267e2e38a67a9be08a9e84e93fe4230a7701762a218e8275edb038733ee56e8",
		"026b11bcbe887507cae1a680247528e1812f58b5341e27399a1d972f0fea6b90",
		"9ede968e005a23ff5274bc49ce2c8dcc24a90a8226252ab7ae8ebb4e20174d36",
		"158675ca97c7064d91478bdd529d9767e2fe56526278cd53b155cd413c656acd",
		"00c1533cd27bf75f2d5869a3d0125ddba037ca8d379b335bd57746d229c31b67",
		"63e8f7c09610d9f80662757b87905e7d69622051b836c6b89c1230a3145b566a",
	}
	if len(inputs) != len(want) {
		t.Fatalf("%d inputs, %d digests", len(inputs), len(want))
	}
	for i, in := range inputs {
		if got := Hash(in...); got != want[i] {
			t.Errorf("Hash(%#v) = %s, want %s", in, got, want[i])
		}
	}
}

type hashNode struct {
	V    int
	Next *hashNode
}

type hashReq struct {
	Model string
	Temp  *float64
	Tags  []string
}

// The bug the reflection walk exists to fix: a pointer, or a struct holding
// one, used to hash as an address, so equal requests never shared a key.
func TestHashFollowsPointersNotAddresses(t *testing.T) {
	t1, t2 := 0.2, 0.2
	a := hashReq{Model: "m", Temp: &t1, Tags: []string{"x"}}
	b := hashReq{Model: "m", Temp: &t2, Tags: []string{"x"}}
	if Hash(a) != Hash(b) {
		t.Error("equal structs holding different pointers to equal values hash differently")
	}
	if Hash(&a) != Hash(&b) {
		t.Error("pointers to equal structs hash differently")
	}
	t2 = 0.3
	if Hash(a) == Hash(b) {
		t.Error("a change behind a pointer did not change the key")
	}
	if Hash((*hashReq)(nil)) == Hash(&hashReq{}) {
		t.Error("a nil pointer hashes like a pointer to the zero value")
	}
	if Hash(a) == Hash(&a) {
		t.Error("a struct and a pointer to it share a key")
	}
}

func TestHashSeparatesTypesWithEqualContents(t *testing.T) {
	type model string
	type x struct{ A int }
	type y struct{ A int }
	if Hash(x{1}) == Hash(y{1}) {
		t.Error("two struct types with equal contents collide")
	}
	if Hash(model("gpt")) == Hash("gpt") {
		t.Error("a named string type collides with string")
	}
	if Hash(struct{ A int }{1}) == Hash(struct{ B int }{1}) {
		t.Error("anonymous structs with different field names collide")
	}
}

// Unnamed composite types are named by their structure.
func TestHashNamesUnnamedTypes(t *testing.T) {
	if Hash([2]int{1, 2}) == Hash([2]int8{1, 2}) {
		t.Error("arrays of different element types collide")
	}
	if Hash(map[int]string{1: "a"}) == Hash(map[int8]string{1: "a"}) {
		t.Error("maps of different key types collide")
	}
	if Hash([]*hashNode{{V: 1}}) != Hash([]*hashNode{{V: 1}}) {
		t.Error("equal slices of pointers hash differently")
	}
}

// Cycles through a pointer, a slice or a map are written as back-references,
// so they terminate, and structurally equal cycles share a key.
func TestHashTerminatesOnCycles(t *testing.T) {
	ring := func(v int) *hashNode {
		n := &hashNode{V: v}
		n.Next = &hashNode{V: v + 1, Next: n}
		return n
	}
	if Hash(ring(1)) != Hash(ring(1)) {
		t.Error("equal pointer cycles hash differently")
	}
	if Hash(ring(1)) == Hash(ring(2)) {
		t.Error("different pointer cycles collide")
	}

	type selfSlice []selfSlice
	s := make(selfSlice, 1)
	s[0] = s
	u := make(selfSlice, 1)
	u[0] = u
	if Hash(s) != Hash(u) {
		t.Error("equal slice cycles hash differently")
	}

	type selfMap map[string]selfMap
	m := selfMap{}
	m["a"] = m
	if Hash(m) == Hash(selfMap{"a": selfMap{"a": nil}}) {
		t.Error("a map cycle collides with its two-level unrolling")
	}
	if Hash(m) == Hash(selfMap{"b": m}) {
		t.Error("map cycles under different keys collide")
	}
}

func TestHashReflectsEveryKind(t *testing.T) {
	type blob []byte
	type kinds struct {
		B   bool
		I   int16
		U   uintptr
		F   float32
		C   complex128
		S   string
		A   [2]int8
		Bs  blob
		Sl  []uint
		M   map[int]string
		Any any
		Nil any
		hid any
	}
	mk := func() kinds {
		return kinds{true, -3, 9, 1.5, complex(1, -2), "s", [2]int8{1, 2}, blob("raw"),
			[]uint{4}, map[int]string{3: "c", 1: "a", 2: "b"}, map[string]any{"k": 1}, nil, []int{7}}
	}
	if Hash(mk()) != Hash(mk()) {
		t.Fatal("equal values of every kind hash differently")
	}
	for name, change := range map[string]func(*kinds){
		"bool":      func(k *kinds) { k.B = false },
		"int":       func(k *kinds) { k.I = 3 },
		"uintptr":   func(k *kinds) { k.U = 8 },
		"float":     func(k *kinds) { k.F = 2.5 },
		"complex":   func(k *kinds) { k.C = complex(1, 2) },
		"string":    func(k *kinds) { k.S = "t" },
		"array":     func(k *kinds) { k.A[1] = 3 },
		"bytes":     func(k *kinds) { k.Bs = blob("rax") },
		"slice":     func(k *kinds) { k.Sl = nil },
		"map":       func(k *kinds) { k.M[1] = "z" },
		"interface": func(k *kinds) { k.Any = map[string]any{"k": 2} },
		"nil iface": func(k *kinds) { k.Nil = 0 },
		"hidden":    func(k *kinds) { k.hid = []int{8} },
	} {
		v := mk()
		change(&v)
		if Hash(v) == Hash(mk()) {
			t.Errorf("changing the %s field did not change the key", name)
		}
	}
	// A value behind an interface field is tagged as Hash tags it directly.
	if Hash(struct{ A any }{"x"}) == Hash(struct{ A any }{[]byte("x")}) {
		t.Error("an interface holding a string collides with one holding bytes")
	}
}

type textErr struct{ N int }

func (textErr) MarshalText() ([]byte, error) { return nil, errors.New("no text") }

type textSame struct{ N int }

func (textSame) MarshalText() ([]byte, error) { return []byte("same"), nil }

// A type with a canonical text form is hashed by it, not by its internals.
func TestHashUsesMarshalText(t *testing.T) {
	now := time.Now() // carries a monotonic reading
	if Hash(now) != Hash(now.Round(0)) {
		t.Error("the monotonic reading changed a time.Time key")
	}
	if Hash(now) == Hash(now.Add(time.Nanosecond)) {
		t.Error("different instants share a key")
	}
	if Hash(&now) == Hash(new(time.Time)) {
		t.Error("pointers to different instants share a key")
	}
	if Hash((*time.Time)(nil)) == Hash(new(time.Time)) {
		t.Error("a nil *time.Time hashes like the zero time")
	}
	if Hash(textSame{1}) != Hash(textSame{2}) {
		t.Error("MarshalText was not used")
	}
	if Hash(textErr{1}) == Hash(textErr{2}) {
		t.Error("a MarshalText error did not fall back to the contents")
	}
	type hidden struct{ at time.Time }
	if Hash(hidden{now}) == Hash(hidden{now.Add(time.Second)}) {
		t.Error("an unexported time field was not hashed")
	}
	type iface struct{ M encoding.TextMarshaler }
	if Hash(iface{}) == Hash(iface{textSame{}}) {
		t.Error("a nil TextMarshaler interface collides with a set one")
	}
}

// Channels and funcs have only an identity, so that is what is hashed.
func TestHashUsesIdentityForChansAndFuncs(t *testing.T) {
	a, b := make(chan int), make(chan int)
	if Hash(a) != Hash(a) || Hash(a) == Hash(b) {
		t.Error("a channel must hash as itself and only itself")
	}
	f := func() {}
	if Hash(f) != Hash(f) {
		t.Error("a func must hash stably")
	}
}

// An unsigned integer is tagged like every other part. Untagged, its 8 bytes
// were the same as eight nil parts ('z' is 0x7a), so these two keys collided.
func TestUnsignedIntegersAreTagged(t *testing.T) {
	if Hash(uint64(0x7a7a7a7a7a7a7a7a)) == Hash(nil, nil, nil, nil, nil, nil, nil, nil) {
		t.Fatal("an unsigned integer collides with eight nils")
	}
	if Hash(uint(5)) == Hash(int(5)) {
		t.Fatal("uint and int of the same value share a key")
	}
	if Hash(uint8(5)) != Hash(uint64(5)) {
		t.Fatal("unsigned widths should hash alike, as the doc says")
	}
}
