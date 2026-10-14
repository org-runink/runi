package toon

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Encode and Decode used to reach every struct through encoding/json, and now
// walk it with reflection instead. Nothing about the result may change: these
// properties generate random struct TYPES -- tags, omitempty, "-", invalid tag
// names, colliding names, pointers, maps, slices, `any`, []byte, Go arrays,
// types with their own marshalers -- and random values of them, and require the
// reflection route and the encoding/json route to agree byte for byte, value
// for value and error for error.

// genType returns a random type. depth bounds nesting.
func genType(r *rand.Rand, depth int) reflect.Type {
	leaves := []reflect.Type{
		reflect.TypeFor[bool](), reflect.TypeFor[int](), reflect.TypeFor[int8](),
		reflect.TypeFor[int16](), reflect.TypeFor[int32](), reflect.TypeFor[int64](),
		reflect.TypeFor[uint](), reflect.TypeFor[uint8](), reflect.TypeFor[uint16](),
		reflect.TypeFor[uint32](), reflect.TypeFor[uint64](), reflect.TypeFor[float32](),
		reflect.TypeFor[float64](), reflect.TypeFor[string](), reflect.TypeFor[[]byte](),
		reflect.TypeFor[any](), reflect.TypeFor[time.Time](), reflect.TypeFor[json.Number](),
	}
	if depth <= 0 || r.IntN(3) == 0 {
		return leaves[r.IntN(len(leaves))]
	}
	switch r.IntN(6) {
	case 0:
		return reflect.SliceOf(genType(r, depth-1))
	case 1:
		return reflect.MapOf(reflect.TypeFor[string](), genType(r, depth-1))
	case 2:
		return reflect.PointerTo(genType(r, depth-1))
	case 3:
		if r.IntN(4) == 0 {
			return reflect.ArrayOf(2, genType(r, depth-1))
		}
		return reflect.MapOf(reflect.TypeFor[int](), genType(r, depth-1))
	default:
		return genStruct(r, depth-1)
	}
}

func genStruct(r *rand.Rand, depth int) reflect.Type {
	tags := []string{
		``, `json:"a"`, `json:"b,omitempty"`, `json:",omitempty"`, `json:"-"`,
		`json:"-,"`, `json:"A"`, `json:"bad\\name"`, `json:"c"`, `json:"Id"`,
	}
	if r.IntN(12) == 0 {
		tags = append(tags, `json:"d,string"`) // left to encoding/json
	}
	n := 1 + r.IntN(4)
	fields := make([]reflect.StructField, n)
	for i := range fields {
		fields[i] = reflect.StructField{
			Name: fmt.Sprintf("F%d", i),
			Type: genType(r, depth),
			Tag:  reflect.StructTag(tags[r.IntN(len(tags))]),
		}
	}
	return reflect.StructOf(fields)
}

// genValue fills a new value of type t.
func genValue(r *rand.Rand, t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	if r.IntN(6) == 0 {
		return v // the zero value: nil pointers, nil maps, empty strings
	}
	switch t.Kind() {
	case reflect.Bool:
		v.SetBool(r.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(r.Uint64() >> (64 - t.Bits())))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(r.Uint64() >> (64 - t.Bits()))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(genFloat(r))
	case reflect.String:
		if t == reflect.TypeFor[json.Number]() {
			v.SetString(fmt.Sprint(r.IntN(1000) - 500))
			break
		}
		v.SetString(genString(r))
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			b := make([]byte, r.IntN(5))
			for i := range b {
				b[i] = byte(r.IntN(256))
			}
			v.SetBytes(b)
			break
		}
		n := r.IntN(4)
		s := reflect.MakeSlice(t, n, n)
		for i := 0; i < n; i++ {
			s.Index(i).Set(genValue(r, t.Elem(), depth+1))
		}
		v.Set(s)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			v.Index(i).Set(genValue(r, t.Elem(), depth+1))
		}
	case reflect.Map:
		m := reflect.MakeMap(t)
		for i := 0; i < r.IntN(4); i++ {
			var k reflect.Value
			if t.Key().Kind() == reflect.String {
				k = reflect.ValueOf(genString(r))
			} else {
				k = reflect.ValueOf(r.IntN(200) - 100)
			}
			m.SetMapIndex(k, genValue(r, t.Elem(), depth+1))
		}
		v.Set(m)
	case reflect.Pointer:
		v.Set(reflect.New(t.Elem()))
		v.Elem().Set(genValue(r, t.Elem(), depth+1))
	case reflect.Interface:
		if a := genAny(r, depth); a != nil {
			v.Set(reflect.ValueOf(a))
		}
	case reflect.Struct:
		if t == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Unix(int64(r.IntN(1<<31)), 0).UTC()))
			break
		}
		for i := 0; i < t.NumField(); i++ {
			v.Field(i).Set(genValue(r, t.Field(i).Type, depth+1))
		}
	}
	return v
}

// genAny is a value for an `any` field: generic shapes and a few Go types.
func genAny(r *rand.Rand, depth int) any {
	if depth > 3 {
		return genString(r)
	}
	switch r.IntN(8) {
	case 0:
		return genFloat(r)
	case 1:
		return r.IntN(1 << 20)
	case 2:
		return genString(r)
	case 3:
		return r.IntN(2) == 0
	case 4:
		return map[string]any{"x": genAny(r, depth+1), "y": genAny(r, depth+1)}
	case 5:
		return []any{genAny(r, depth+1), genAny(r, depth+1)}
	case 6:
		return []string{genString(r)}
	default:
		return nil
	}
}

func genFloat(r *rand.Rand) float64 {
	switch r.IntN(8) {
	case 0:
		return math.Copysign(0, -1)
	case 1:
		return 1e-7 * float64(1+r.IntN(9))
	case 2:
		return 1e21 * float64(1+r.IntN(9))
	case 3:
		return float64(r.IntN(1<<20)) / 8
	case 4:
		return float64(r.Int64N(1<<62)) - (1 << 61) // large, beyond 2^53
	default:
		return (r.Float64() - 0.5) * 1e6
	}
}

func genString(r *rand.Rand) string {
	pool := []string{"", "plain", "a,b", "a|b", "a\tb", "x: y", `q"uote`, "true", "12", "1.5",
		"- dash", " pad ", "new\nline", "naïve", "\xff\xfe", "Kelvin K", "{x}", "[y]", "back\\slash"}
	return pool[r.IntN(len(pool))]
}

// encodeViaJSON is Encode as it was before the reflection walk: every value
// that is not already a generic tree goes through encoding/json.
func encodeViaJSON(v any, delim byte) (string, error) {
	doc, direct := genericTree(v)
	if !direct {
		var err error
		if doc, err = viaJSON(v); err != nil {
			return "", err
		}
	}
	return encodeGeneric(doc, delim)
}

// decodeViaJSON is Decode into a typed destination as it was before: parse,
// render to JSON, let encoding/json fill the destination.
func decodeViaJSON(text string, v any) error {
	doc, err := parse(text, false, false)
	if err != nil {
		return err
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("toon: %w", err)
	}
	return json.Unmarshal(b, v)
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func TestPropertyReflectEncodeMatchesJSONRoute(t *testing.T) {
	r := rand.New(rand.NewPCG(901, 902))
	for i := 0; i < 6000; i++ {
		typ := genStruct(r, 2)
		v := genValue(r, typ, 0).Interface()
		delim := []byte{',', '\t', '|'}[r.IntN(3)]

		fast, ferr := EncodeWith(v, Options{Delimiter: rune(delim)})
		slow, serr := encodeViaJSON(v, delim)
		if errText(ferr) != errText(serr) {
			t.Fatalf("%v\nreflect err: %v\njson err:    %v", typ, ferr, serr)
		}
		if fast != slow {
			t.Fatalf("%v\n--- reflect ---\n%s--- json ---\n%s", typ, fast, slow)
		}
	}
}

func TestPropertyReflectDecodeMatchesJSONRoute(t *testing.T) {
	r := rand.New(rand.NewPCG(903, 904))
	decoded := 0
	for i := 0; i < 6000; i++ {
		typ := genStruct(r, 2)
		// The text is written from a value of the same type most of the time,
		// and from a different random type otherwise, so mismatches -- a
		// string where a number belongs, a list where an object does -- are
		// decoded too and must fail identically.
		src := typ
		if r.IntN(3) == 0 {
			src = genStruct(r, 2)
		}
		text, err := Encode(genValue(r, src, 0).Interface())
		if err != nil {
			continue
		}

		// Both destinations start from the SAME non-zero value, because
		// json.Unmarshal merges into what is there and so must this.
		seed := r.Uint64()
		fast := reflect.New(typ)
		fast.Elem().Set(genValue(rand.New(rand.NewPCG(seed, 1)), typ, 0))
		slow := reflect.New(typ)
		slow.Elem().Set(genValue(rand.New(rand.NewPCG(seed, 1)), typ, 0))

		ferr := Decode(text, fast.Interface())
		serr := decodeViaJSON(text, slow.Interface())
		if errText(ferr) != errText(serr) {
			t.Fatalf("%v\nreflect err: %v\njson err:    %v\n--- text ---\n%s", typ, ferr, serr, text)
		}
		if !reflect.DeepEqual(fast.Interface(), slow.Interface()) {
			t.Fatalf("%v\nreflect: %#v\njson:    %#v\n--- text ---\n%s", typ, fast.Elem(), slow.Elem(), text)
		}
		if ferr == nil {
			decoded++
		}
	}
	if decoded < 2000 {
		t.Fatalf("only %d of 6000 cases decoded cleanly; the property is mostly testing errors", decoded)
	}
}

// The generated documents above never contain NaN, a fraction where an
// integer belongs or an out-of-range number, because Encode never writes them
// from a value of the right type. A model does. Each is decoded both ways.
func TestReflectDecodeMatchesJSONOnHandWrittenInput(t *testing.T) {
	type dst struct {
		I   int            `json:"i"`
		I8  int8           `json:"i8"`
		U   uint           `json:"u"`
		U8  uint8          `json:"u8"`
		F32 float32        `json:"f32"`
		F64 float64        `json:"f64"`
		S   string         `json:"s"`
		B   bool           `json:"b"`
		A   any            `json:"a"`
		P   *int           `json:"p"`
		M   map[string]int `json:"m"`
		L   []int          `json:"l"`
		Ar  [2]int         `json:"ar"`
		N   json.Number    `json:"n"`
		T   time.Time      `json:"t"`
		IM  map[int]string `json:"im"`
		Q   fmt.Stringer   `json:"q"`
		By  []byte         `json:"by"`
	}
	for _, text := range []string{
		"i: 3.0", "i: 2.5", "i: 1e3", "i: 99999999999999999999", "i8: 300", "i8: -129",
		"u: -1", "u: 4.0", "u: 1.5", "u: 18446744073709551615", "u: 18446744073709551616",
		"u8: 256", "u8: 255",
		"f32: 1e39", "f32: 16777217", "f32: 0.1", "f32: 3", "f32: 9007199254740993",
		"f64: NaN", "f64: 1e400", "f64: 9007199254740993", "f64: -0", "f64: 2",
		"f32: 99999999999999999999999999999999999999999",
		"i: true", "i: x", "u: x", "f64: x", "f64: true", "s: 1", "b: 1", "b: true",
		"a: NaN", "a:\n  x: NaN", "a[2]: 1,NaN", "a[2]: 1,2", "a:\n  x: 9007199254740993",
		"p: 4", "p: null", "m:\n  x: 1\n  y: z", "m: null", "m: 3", "l[2]: 1,x", "l: null", "l: 3",
		"l[0]:", "ar[2]: 1,2", "n: 5", "t: \"2024-01-02T03:04:05Z\"", "im:\n  1: a",
		"q: x", "q: null", "by: \"AQI=\"", "by[2]: 1,2", "I: 5", "i: 1\nI: 2",
		"unknown: 1\ni: 7", "s: null", "i: null",
	} {
		var fast, slow dst
		fast.P, slow.P = new(int), new(int)
		fast.M, slow.M = map[string]int{"keep": 1}, map[string]int{"keep": 1}
		fast.L, slow.L = make([]int, 1, 4), make([]int, 1, 4)
		ferr := Decode(text, &fast)
		serr := decodeViaJSON(text, &slow)
		if errText(ferr) != errText(serr) {
			t.Errorf("%q\nreflect err: %v\njson err:    %v", text, ferr, serr)
			continue
		}
		if !reflect.DeepEqual(fast, slow) && !bothNaN(fast.F64, slow.F64) {
			t.Errorf("%q\nreflect: %+v\njson:    %+v", text, fast, slow)
		}
	}
}

func bothNaN(a, b float64) bool { return math.IsNaN(a) && math.IsNaN(b) }

// The point of the walk is that ordinary values never touch encoding/json. If
// a refactor sent them back there, every test above would still pass -- the
// results are identical by design -- and only the speed would quietly go. So
// this pins the route itself for the shapes an inference payload is made of.
func TestOrdinaryValuesDoNotNeedJSON(t *testing.T) {
	type row struct {
		ID    int               `json:"id"`
		Label string            `json:"label,omitempty"`
		Score float32           `json:"score"`
		Tags  []string          `json:"tags"`
		Meta  map[string]any    `json:"meta"`
		Next  *row              `json:"next"`
		Blob  []byte            `json:"blob"`
		Seen  map[uint16]string `json:"seen"`
		Grid  [2]int            `json:"grid"`
		Skip  string            `json:"-"`
		quiet int
	}
	v := row{ID: 1, Tags: []string{"a"}, Meta: map[string]any{"k": 1.5}, Next: &row{ID: 2},
		Blob: []byte{1}, Seen: map[uint16]string{7: "x"}, quiet: 3}
	if _, err := toTree(reflect.ValueOf(v), 0); err != nil {
		t.Fatalf("toTree declined an ordinary value: %v", err)
	}
	text, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse(text, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		ID    int            `json:"id"`
		Score float32        `json:"score"`
		Tags  []string       `json:"tags"`
		Meta  map[string]any `json:"meta"`
		Next  *struct {
			ID int `json:"id"`
		} `json:"next"`
	}
	if err := fromTree(doc, reflect.ValueOf(&back).Elem(), true); err != nil {
		t.Fatalf("fromTree declined an ordinary destination: %v", err)
	}
	_ = fromTree(doc, reflect.ValueOf(&back).Elem(), false)
	if back.ID != 1 || back.Next == nil || back.Next.ID != 2 || back.Meta["k"] != 1.5 || back.Tags[0] != "a" {
		t.Fatalf("decoded %+v", back)
	}
}

// Each shape the walk leaves to encoding/json is declined, not attempted.
func TestShapesLeftToJSONAreDeclined(t *testing.T) {
	type inner struct{ X int }
	type embeds struct {
		inner
		Y int
	}
	// Built at run time: go vet rightly rejects two fields with one tag in
	// source, and the point here is that such a type is handed to json.
	dup := reflect.New(reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int](), Tag: `json:"same"`},
		{Name: "B", Type: reflect.TypeFor[int](), Tag: `json:"same"`},
	})).Elem().Interface()
	type omitzero struct {
		A int `json:"a,omitzero"`
	}
	type stringOpt struct {
		A int `json:"a,string"`
	}
	nan := math.NaN()
	ch := make(chan int)
	deep := any(nil)
	for i := 0; i < maxDepth+2; i++ {
		deep = []any{deep}
	}
	for name, v := range map[string]any{
		"embedded":       embeds{},
		"duplicate name": dup,
		"omitzero":       omitzero{},
		",string":        stringOpt{},
		"Marshaler":      time.Time{},
		"json.Number":    struct{ N json.Number }{"1"},
		"NaN":            struct{ F float64 }{nan},
		"float32 Inf":    struct{ F float32 }{float32(math.Inf(1))},
		"chan":           struct{ C chan int }{ch},
		"complex":        struct{ C complex64 }{1},
		"func key":       map[float64]int{1: 1},
		"text key":       map[textKey]int{1: 1},
		"too deep":       struct{ D any }{deep},
		"marshaler elem": struct{ B []textKey }{[]textKey{1}},
		"colliding keys": map[string]int{"\xff": 1, "\xfe": 2},
	} {
		if _, err := toTree(reflect.ValueOf(v), 0); err != errNeedsJSON {
			t.Errorf("%s: toTree returned %v, want errNeedsJSON", name, err)
		}
	}
}

type textKey int

func (k textKey) MarshalText() ([]byte, error) { return []byte(fmt.Sprint("k", int(k))), nil }

// A destination the walk declines is still filled -- by encoding/json -- so
// Decode supports exactly what it supported before.
func TestDeclinedDestinationsStillDecode(t *testing.T) {
	var got struct {
		When time.Time      `json:"when"`
		Keys map[int]string `json:"keys"`
		Pair [2]string      `json:"pair"`
		Raw  []byte         `json:"raw"`
	}
	text := "when: \"2024-05-06T07:08:09Z\"\nkeys:\n  3: three\npair[2]: a,b\nraw: \"AQID\"\n"
	if err := Decode(text, &got); err != nil {
		t.Fatal(err)
	}
	if got.When.Year() != 2024 || got.Keys[3] != "three" || got.Pair[1] != "b" || len(got.Raw) != 3 {
		t.Fatalf("got %+v", got)
	}
}

// Decode into something that is not a non-nil pointer is encoding/json's
// InvalidUnmarshalError, as before.
func TestDecodeIntoANonPointerIsJSONsError(t *testing.T) {
	var s struct{ A int }
	err := Decode("A: 1\n", s)
	if _, ok := err.(*json.InvalidUnmarshalError); !ok {
		t.Fatalf("got %T %v", err, err)
	}
	var p *struct{ A int }
	if _, ok := Decode("A: 1\n", p).(*json.InvalidUnmarshalError); !ok {
		t.Fatal("a nil pointer was not refused")
	}
}

func TestValidStringRepairsLikeJSON(t *testing.T) {
	for _, s := range []string{"ok", "\xff", "a\xffb\xfe", "é\xc3", "\xe2\x82"} {
		b, _ := json.Marshal(s)
		var want string
		_ = json.Unmarshal(b, &want)
		if got := validString(s); got != want {
			t.Errorf("validString(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestValidTag(t *testing.T) {
	for tag, want := range map[string]bool{
		"": false, "name": true, "with space": true, "a-b.c": true, "ünï": true,
		`back\slash`: false, "quote\"": false, "comma,": false,
	} {
		if validTag(tag) != want {
			t.Errorf("validTag(%q) = %v, want %v", tag, !want, want)
		}
	}
}

func TestEmptyValueMatchesOmitempty(t *testing.T) {
	type all struct {
		A [0]int         `json:"a,omitempty"`
		B bool           `json:"b,omitempty"`
		I int            `json:"i,omitempty"`
		U uint           `json:"u,omitempty"`
		F float64        `json:"f,omitempty"`
		N float64        `json:"n,omitempty"` // -0 is empty: it equals 0
		P *int           `json:"p,omitempty"`
		X any            `json:"x,omitempty"`
		M map[string]int `json:"m,omitempty"`
		S string         `json:"s,omitempty"`
		T struct{}       `json:"t,omitempty"` // a struct is never empty
	}
	v := all{N: math.Copysign(0, -1)}
	got, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	want, err := encodeViaJSON(v, ',')
	if err != nil {
		t.Fatal(err)
	}
	if got != want || strings.TrimSpace(got) != "t:" {
		t.Fatalf("got %q, json route %q", got, want)
	}
}
