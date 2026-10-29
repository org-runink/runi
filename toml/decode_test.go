// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// A configuration file of the shape connection manifests take: arrays of
// tables nested two deep, free-form settings, and SQL in multi-line strings.
const manifest = `
name = "warehouse"

[[connections]]
name = "orders"
kind = "postgres"
fromEnv = "ORDERS_DSN"
query = '''
SELECT id, total
  FROM orders
 WHERE placed_at > now() - interval '1 day'
'''
[connections.settings]
port = 5432
hosts = ["db-1", "db-2"]
tls = { mode = "verify-full" }
[[connections.credentials]]
user = "reader"
[[connections.credentials]]
user = "writer"

[[connections]]
name = "events"
kind = "kafka"
query = """
SELECT *
  FROM "events" \
 LIMIT 10"""
[connections.settings]
port = 9092

[labels]
team = "data"
tier = "gold"
`

type credential struct {
	User string `toml:"user"`
}

type connection struct {
	Name        string         `toml:"name"`
	Kind        string         `toml:"kind"`
	FromEnv     string         `toml:"fromEnv"`
	Query       string         `toml:"query"`
	Settings    map[string]any `toml:"settings"`
	Credentials []credential   `toml:"credentials"`
}

type manifestDoc struct {
	Name        string            `toml:"name"`
	Connections []connection      `toml:"connections"`
	Labels      map[string]string `toml:"labels"`
}

func TestDecodeManifest(t *testing.T) {
	var doc manifestDoc
	if err := Unmarshal([]byte(manifest), &doc); err != nil {
		t.Fatal(err)
	}
	want := manifestDoc{
		Name: "warehouse",
		Connections: []connection{
			{
				Name: "orders", Kind: "postgres", FromEnv: "ORDERS_DSN",
				Query:       "SELECT id, total\n  FROM orders\n WHERE placed_at > now() - interval '1 day'\n",
				Settings:    map[string]any{"port": int64(5432), "hosts": []any{"db-1", "db-2"}, "tls": map[string]any{"mode": "verify-full"}},
				Credentials: []credential{{User: "reader"}, {User: "writer"}},
			},
			{
				Name: "events", Kind: "kafka",
				Query:    "SELECT *\n  FROM \"events\" LIMIT 10",
				Settings: map[string]any{"port": int64(9092)},
			},
		},
		Labels: map[string]string{"team": "data", "tier": "gold"},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("got  %#v\nwant %#v", doc, want)
	}
}

func TestDecodeErrorPath(t *testing.T) {
	type settings struct {
		Port int `toml:"port"`
	}
	var doc struct {
		Connections []struct {
			Name     string   `toml:"name"`
			Settings settings `toml:"settings"`
		} `toml:"connections"`
	}
	src := "[[connections]]\nname = \"a\"\n[connections.settings]\nport = 1\n\n[[connections]]\nname = \"b\"\n[connections.settings]\nport = \"5432\"\n"
	err := Unmarshal([]byte(src), &doc)
	const want = `toml: connections[1].settings.port: expected integer, got string (line 9:1)`
	if err == nil || err.Error() != want {
		t.Fatalf("got  %v\nwant %s", err, want)
	}
	var e *Error
	if !errors.As(err, &e) || e.Path != "connections[1].settings.port" || e.Line != 9 || e.Column != 1 {
		t.Fatalf("got %#v", e)
	}
}

func TestDecodeErrorPathInArray(t *testing.T) {
	var doc struct {
		Ports []int `toml:"ports"`
	}
	err := Unmarshal([]byte("ports = [1, 2,\n  \"x\"]"), &doc)
	const want = `toml: ports[2]: expected integer, got string (line 2:3)`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeQuotedKeyInPath(t *testing.T) {
	var doc map[string]map[string]int
	err := Unmarshal([]byte("[\"a.b\"]\n\"\" = \"x\""), &doc)
	if err == nil || !strings.Contains(err.Error(), `toml: "a.b"."": expected integer`) {
		t.Fatalf("got %v", err)
	}
}

func TestStrictUnknownKeys(t *testing.T) {
	type conn struct {
		Name string `toml:"name"`
	}
	var doc struct {
		Connections []conn            `toml:"connections"`
		Extra       map[string]any    `toml:"extra"`
		Labels      map[string]string `toml:"labels"`
	}
	src := "nmae = 1\n[[connections]]\nname = \"a\"\nhost = \"h\"\n[extra]\nanything = 1\n[unknown.table]\nx = 1\n"
	d := NewDecoder(strings.NewReader(src))
	d.DisallowUnknownFields()
	err := d.Decode(&doc)
	if err == nil {
		t.Fatal("unknown keys were accepted")
	}
	for _, want := range []string{
		`toml: nmae: unknown key: the struct has no field for it`,
		`(line 1:1)`,
		`toml: connections[0].host: unknown key: toml.conn has no field for it`,
		`(line 4:1)`,
		`toml: unknown: unknown key`,
		`(line 7:2)`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q is missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "anything") || strings.Contains(err.Error(), "unknown.table") {
		t.Errorf("reported keys inside a map or below an unknown table: %v", err)
	}
	var e *Error
	if !errors.As(err, &e) || e.Path != "nmae" {
		t.Errorf("errors.As found %#v", e)
	}
	// The known values were still decoded.
	if doc.Connections[0].Name != "a" || doc.Extra["anything"] != int64(1) {
		t.Errorf("got %#v", doc)
	}

	// Without the option the same document decodes cleanly.
	if err := Unmarshal([]byte(src), &doc); err != nil {
		t.Errorf("non-strict: %v", err)
	}
}

func TestStrictReportsTypeErrorsFirst(t *testing.T) {
	var doc struct {
		A int `toml:"a"`
	}
	d := NewDecoder(strings.NewReader("x = 1\na = \"s\""))
	d.DisallowUnknownFields()
	err := d.Decode(&doc)
	if err == nil || !strings.Contains(err.Error(), "a: expected integer, got string") || strings.Contains(err.Error(), "unknown") {
		t.Fatalf("got %v", err)
	}
}

func TestCaseSensitiveKeys(t *testing.T) {
	var doc struct {
		FromEnv string `toml:"fromEnv"`
		Name    string
	}
	src := "fromenv = \"x\"\nFROMENV = \"y\"\nname = \"lower\"\nName = \"exact\""
	if err := Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.FromEnv != "" || doc.Name != "exact" {
		t.Fatalf("got %#v", doc)
	}
	d := NewDecoder(strings.NewReader(src))
	d.DisallowUnknownFields()
	err := d.Decode(&doc)
	for _, k := range []string{"fromenv: unknown key", "FROMENV: unknown key", "name: unknown key"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%v is missing %q", err, k)
		}
	}
}

func TestDecodeFunctionAndReaderError(t *testing.T) {
	var v map[string]int
	if err := Decode(strings.NewReader("a = 1"), &v); err != nil || v["a"] != 1 {
		t.Fatalf("got %v %v", v, err)
	}
	boom := errors.New("boom")
	if err := Decode(iotest.ErrReader(boom), &v); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if err := Decode(strings.NewReader("a = "), &v); err == nil {
		t.Fatal("a syntax error was accepted")
	}
}

func TestInvalidDestination(t *testing.T) {
	var p *struct{}
	for _, v := range []any{nil, struct{}{}, p} {
		if err := Unmarshal([]byte("a = 1"), v); !errors.Is(err, ErrInvalid) {
			t.Errorf("Unmarshal(%T) = %v, want ErrInvalid", v, err)
		}
	}
}

func TestDecodeRoots(t *testing.T) {
	var anyv any
	if err := Unmarshal([]byte("a = [1, {b = 'c'}]"), &anyv); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": []any{int64(1), map[string]any{"b": "c"}}}
	if !reflect.DeepEqual(anyv, want) {
		t.Fatalf("got %#v", anyv)
	}
	existing := map[string]any{"keep": true}
	if err := Unmarshal([]byte("a = 1"), &existing); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(existing, map[string]any{"keep": true, "a": int64(1)}) {
		t.Fatalf("got %#v", existing)
	}
	type name string
	var named map[name]int
	if err := Unmarshal([]byte("x = 1"), &named); err != nil || named["x"] != 1 {
		t.Fatalf("got %v %v", named, err)
	}
	var n int
	if err := Unmarshal([]byte("a = 1"), &n); err == nil || err.Error() != "toml: expected integer, got table (line 1:1)" {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeScalarsAndPointers(t *testing.T) {
	type inner struct{ V int }
	var doc struct {
		I8   int8
		U16  uint16
		U    uint
		F32  float32
		F64  float64
		FInt float64
		B    bool
		S    string
		P    *int
		PP   **inner
		Any  any
		Arr  [3]int
		Big  [2][]string
	}
	doc.Arr = [3]int{9, 9, 9}
	src := "I8 = -128\nU16 = 65535\nU = 7\nF32 = 1.5\nF64 = -inf\nFInt = 9007199254740992\nB = true\nS = 'x'\nP = 4\nPP = {V = 2}\nAny = 1979-05-27\nArr = [1, 2]\nBig = [['a'], []]"
	if err := Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.I8 != -128 || doc.U16 != 65535 || doc.U != 7 || doc.F32 != 1.5 || !math.IsInf(doc.F64, -1) ||
		doc.FInt != 1<<53 || !doc.B || doc.S != "x" || *doc.P != 4 || (**doc.PP).V != 2 ||
		doc.Any != (LocalDate{1979, 5, 27}) || doc.Arr != [3]int{1, 2, 0} || len(doc.Big[0]) != 1 || len(doc.Big[1]) != 0 {
		t.Fatalf("got %#v", doc)
	}
}

type upper string

func (u *upper) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		return errors.New("empty")
	}
	*u = upper(strings.ToUpper(string(b)))
	return nil
}

type stringer interface{ String() string }

func TestDecodeTextAndTime(t *testing.T) {
	var doc struct {
		Addr  netip.Addr
		Up    upper
		ODT   time.Time
		LDT   time.Time
		LD    time.Time
		Str   time.Time
		LDate LocalDate
		LTime LocalTime
		LDTv  LocalDateTime
		Sv    stringer
		PT    *time.Time
	}
	src := "Addr = '192.0.2.1'\nUp = 'abc'\nODT = 1979-05-27T07:32:00-07:00\nLDT = 1979-05-27T07:32:00\nLD = 1979-05-27\nStr = '1979-05-27T07:32:00Z'\n" +
		"LDate = 2000-01-02\nLTime = 03:04:05\nLDTv = 2000-01-02T03:04:05\nSv = 07:00:00\nPT = 2000-01-01T00:00:00Z"
	if err := Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	utc := func(s string) time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	if doc.Addr != netip.MustParseAddr("192.0.2.1") || doc.Up != "ABC" ||
		!doc.ODT.Equal(utc("1979-05-27T14:32:00Z")) ||
		!doc.LDT.Equal(utc("1979-05-27T07:32:00Z")) || doc.LDT.Location() != time.UTC ||
		!doc.LD.Equal(utc("1979-05-27T00:00:00Z")) || doc.LD.Location() != time.UTC || !doc.Str.Equal(utc("1979-05-27T07:32:00Z")) ||
		doc.LDate != (LocalDate{2000, 1, 2}) || doc.LTime != (LocalTime{3, 4, 5, 0}) ||
		doc.LDTv != (LocalDateTime{LocalDate{2000, 1, 2}, LocalTime{3, 4, 5, 0}}) ||
		doc.Sv.String() != "07:00:00" || !doc.PT.Equal(utc("2000-01-01T00:00:00Z")) {
		t.Fatalf("got %#v", doc)
	}
}

func TestDecodeMismatches(t *testing.T) {
	type S struct{ X int }
	cases := []struct {
		dst  any
		src  string
		want string
	}{
		// What each Go type is said to want.
		{new(string), "v = 1", "expected string, got integer"},
		{new(bool), "v = 1", "expected boolean, got integer"},
		{new(int), "v = 'x'", "expected integer, got string"},
		{new(uint8), "v = 1.5", "expected integer, got float"},
		{new(float64), "v = true", "expected float, got boolean"},
		{new(S), "v = 1", "expected table, got integer"},
		{new(map[string]int), "v = 1", "expected table, got integer"},
		{new(map[int]int), "v = {a = 1}", "expected a map with string keys, not map[int]int, got table"},
		{new([]int), "v = {}", "expected array, got table"},
		{new([2]int), "v = 'x'", "expected array, got string"},
		{new(time.Time), "v = 07:00:00", "expected date-time (time.Time), got local time"},
		{new(time.Time), "v = {}", "expected date-time (time.Time), got table"},
		{new(LocalDate), "v = '2000-01-01'", "expected local date, got string"},
		{new(LocalTime), "v = 2000-01-01", "expected local time, got local date"},
		{new(LocalDateTime), "v = 2000-01-01T00:00:00Z", "expected local date-time, got offset date-time"},
		{new(LocalDate), "v = 2000-01-01T00:00:00", "expected local date, got local date-time"},
		{new(chan int), "v = []", "expected a value for Go type chan int, got array"},
		{new(stringer), "v = 1", "expected a value for Go type toml.stringer, got integer"},
		{new(string), "v = [[1]]", "expected string, got array"},

		// Values that do not fit.
		{new(int8), "v = 128", "128 overflows int8"},
		{new(uint), "v = -1", "-1 overflows uint"},
		{new(uint8), "v = 256", "256 overflows uint8"},
		{new(float64), "v = 9007199254740993", "9007199254740993 cannot be held exactly by float64"},
		{new(float32), "v = -9007199254740993", "cannot be held exactly by float32"},
		{new(float32), "v = 1e300", "1e+300 overflows float32"},
		{new([1]int), "v = [1, 2]", "2 elements do not fit in [1]int"},
		{new(upper), "v = ''", "v: empty"},
	}
	for _, c := range cases {
		dst := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "V", Type: reflect.TypeOf(c.dst).Elem(), Tag: `toml:"v"`}}))
		err := Unmarshal([]byte(c.src), dst.Interface())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%T from %q: got %v, want %q", c.dst, c.src, err, c.want)
		}
	}
}

func TestDecodeKindNames(t *testing.T) {
	for src, kind := range map[string]string{
		"v = {}": "table", "v = []": "array", "v = 's'": "string", "v = 1": "integer", "v = 1.0": "float",
		"v = true": "boolean", "v = 2000-01-01T00:00:00Z": "offset date-time", "v = 2000-01-01T00:00:00": "local date-time",
		"v = 2000-01-01": "local date", "v = 00:00:00": "local time",
	} {
		var dst struct {
			V chan int `toml:"v"`
		}
		err := Unmarshal([]byte(src), &dst)
		if err == nil || !strings.HasSuffix(strings.SplitN(err.Error(), " (line", 2)[0], "got "+kind) {
			t.Errorf("%q: got %v, want kind %s", src, err, kind)
		}
	}
}

type Base struct {
	ID   int `toml:"id"`
	Note string
}

type Other struct {
	Note string
	Tag  string `toml:"tag"`
	Only string
}

type Deep struct{ Other }

type Port int

type small int

type Node struct {
	*Node
	Val int
}

type hidden struct{ Secret int }

func TestStructFieldRules(t *testing.T) {
	type withOptions struct {
		Renamed string `toml:"renamed,omitempty"`
		Opt     string `toml:",omitempty"`
		Skip    string `toml:"-"`
		private int
	}
	var w withOptions
	if err := Unmarshal([]byte("renamed = 'a'\nOpt = 'b'\nSkip = 'c'\nprivate = 1\n\"-\" = 2"), &w); err != nil {
		t.Fatal(err)
	}
	if w.Renamed != "a" || w.Opt != "b" || w.Skip != "" || w.private != 0 {
		t.Fatalf("got %#v", w)
	}

	// Promotion: Base.Note and Other.Note collide at equal depth and are
	// ambiguous; Deep's Other.Tag is deeper than Outer's own Tag.
	type Outer struct {
		Base
		*Other
		Deep
		Port
		small
		*hidden
		Tag string `toml:"tag"`
	}
	var o Outer
	src := "id = 1\nNote = 'n'\ntag = 't'\nPort = 80\nsmall = 1\nSecret = 1\nOnly = 'o'"
	if err := Unmarshal([]byte(src), &o); err != nil {
		t.Fatal(err)
	}
	if o.ID != 1 || o.Base.Note != "" || o.Tag != "t" || o.Port != 80 || o.small != 0 || o.hidden != nil {
		t.Fatalf("got %#v", o)
	}
	if o.Other == nil || o.Other.Only != "o" || o.Deep.Only != "" {
		t.Fatalf("the shallower embedded pointer was not allocated and filled: %#v", o.Other)
	}
	d := NewDecoder(strings.NewReader(src))
	d.DisallowUnknownFields()
	err := d.Decode(&o)
	for _, k := range []string{"Note: unknown key", "small: unknown key", "Secret: unknown key"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%v is missing %q", err, k)
		}
	}

	// A tagged field beats an untagged one at the same depth, in either order.
	type TaggedNote struct {
		N string `toml:"Note"`
	}
	type UntaggedFirst struct {
		Base
		TaggedNote
	}
	type TaggedFirst struct {
		TaggedNote
		Base
	}
	var u UntaggedFirst
	var tf TaggedFirst
	if err := Unmarshal([]byte("Note = 'x'"), &u); err != nil || u.N != "x" {
		t.Fatalf("got %#v %v", u, err)
	}
	if err := Unmarshal([]byte("Note = 'x'"), &tf); err != nil || tf.N != "x" {
		t.Fatalf("got %#v %v", tf, err)
	}

	// A shallower field wins even when a deeper one was met first.
	type DeepFirst struct {
		Deep
		Other
	}
	var df DeepFirst
	if err := Unmarshal([]byte("tag = 'shallow'"), &df); err != nil || df.Other.Tag != "shallow" || df.Deep.Tag != "" {
		t.Fatalf("got %#v %v", df, err)
	}

	// A self-embedding type terminates.
	var n Node
	if err := Unmarshal([]byte("Val = 3"), &n); err != nil || n.Val != 3 {
		t.Fatalf("got %#v %v", n, err)
	}
}

func TestFieldCacheIsReused(t *testing.T) {
	type C struct{ A int }
	for i := 0; i < 2; i++ {
		var c C
		if err := Unmarshal([]byte(fmt.Sprintf("A = %d", i)), &c); err != nil || c.A != i {
			t.Fatalf("round %d: %#v %v", i, c, err)
		}
	}
}
