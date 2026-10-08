// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

// The tests in this file check the encoder against the *Apache Avro
// specification*, not against the decoder. Byte-for-byte golden vectors are
// taken from the spec's "Binary Encoding" section (zig-zag varint, length-
// prefixed bytes/strings, little-endian IEEE-754, array blocks terminated by a
// zero count, union branch index as a long).
//
// This matters because store's whole sovereignty claim for recordstore is "any
// Avro reader, anywhere, can read our files with no schema registry". A
// round-trip test cannot detect a self-consistent deviation from the spec: an
// encoder and decoder that are wrong in the same direction round-trip perfectly
// and produce files nobody else can read. Only fixed golden bytes catch that.

func hexOf(t *testing.T, b []byte) string {
	t.Helper()
	return hex.EncodeToString(b)
}

func TestLongIsSpecZigZagVarint(t *testing.T) {
	t.Parallel()
	// Avro spec, "Primitive Types": int and long are written with variable-length
	// zig-zag coding. These pairs are the spec's own worked examples plus the
	// multi-byte boundaries either side of them.
	cases := []struct {
		v    int64
		want string // hex of the encoded bytes
	}{
		{0, "00"},
		{-1, "01"},
		{1, "02"},
		{-2, "03"},
		{2, "04"},
		{-64, "7f"},
		{64, "8001"},
		{63, "7e"},
		{-65, "8101"},
		{8192, "808001"},
		{-8193, "818001"},
		{math.MaxInt64, "feffffffffffffffff01"},
		{math.MinInt64, "ffffffffffffffffff01"},
	}
	for _, c := range cases {
		e := NewEncoder()
		e.Long(c.v)
		if got := hexOf(t, e.Bytes()); got != c.want {
			t.Errorf("Long(%d) encoded as %s, spec requires %s", c.v, got, c.want)
		}
		// And the decoder must read the spec bytes back, independently of what
		// the encoder produced.
		raw, err := hex.DecodeString(c.want)
		if err != nil {
			t.Fatalf("bad test vector %q: %v", c.want, err)
		}
		got, err := NewDecoder(bytes.NewReader(raw)).Long()
		if err != nil {
			t.Errorf("decoding spec bytes %s for %d: %v", c.want, c.v, err)
			continue
		}
		if got != c.v {
			t.Errorf("decoding spec bytes %s yielded %d, want %d", c.want, got, c.v)
		}
	}
}

func TestStringAndBytesAreLengthPrefixed(t *testing.T) {
	t.Parallel()
	// Avro spec: a string is "written as a long followed by that many bytes of
	// UTF-8". The spec's own example: "foo" -> 06 66 6f 6f.
	e := NewEncoder()
	e.String("foo")
	if got := hexOf(t, e.Bytes()); got != "06666f6f" {
		t.Errorf("String(%q) = %s, spec requires 06666f6f", "foo", got)
	}

	// The length prefix is a BYTE count, not a rune count. "é" is two UTF-8
	// bytes, so the prefix must be zigzag(2) = 0x04, not zigzag(1) = 0x02.
	e = NewEncoder()
	e.String("é")
	if got := hexOf(t, e.Bytes()); got != "04c3a9" {
		t.Errorf("String(%q) = %s, want 04c3a9 (byte length 2, not rune length 1)", "é", got)
	}

	e = NewEncoder()
	e.String("")
	if got := hexOf(t, e.Bytes()); got != "00" {
		t.Errorf("String(\"\") = %s, want 00", got)
	}

	e = NewEncoder()
	e.Blob([]byte{0x00, 0xff})
	if got := hexOf(t, e.Bytes()); got != "0400ff" {
		t.Errorf("Blob([00 ff]) = %s, want 0400ff", got)
	}
}

func TestBoolIsSingleByte(t *testing.T) {
	t.Parallel()
	e := NewEncoder()
	e.Bool(true)
	e.Bool(false)
	if got := hexOf(t, e.Bytes()); got != "0100" {
		t.Errorf("Bool(true);Bool(false) = %s, spec requires 0100", got)
	}
	// The decoder must treat *any* non-zero byte as true, and it must not
	// confuse a boolean with a zig-zag long (zigzag would read 0x01 as -1).
	for _, tc := range []struct {
		b    byte
		want bool
	}{{0x00, false}, {0x01, true}, {0x02, true}, {0xff, true}} {
		got, err := NewDecoder(bytes.NewReader([]byte{tc.b})).Bool()
		if err != nil {
			t.Fatalf("Bool(%#x): %v", tc.b, err)
		}
		if got != tc.want {
			t.Errorf("decoding boolean byte %#x = %v, want %v", tc.b, got, tc.want)
		}
	}
}

func TestFloatAndDoubleAreLittleEndianIEEE754(t *testing.T) {
	t.Parallel()
	// Avro spec: float is "the 32-bit IEEE-754 layout ... in little-endian
	// order"; double likewise for 64 bits. Big-endian would round-trip fine and
	// be unreadable by every other Avro implementation, which is exactly the
	// failure a round-trip test misses.
	e := NewEncoder()
	e.Double(1.0)
	if got := hexOf(t, e.Bytes()); got != "000000000000f03f" {
		t.Errorf("Double(1.0) = %s, spec requires 000000000000f03f (little-endian)", got)
	}
	e = NewEncoder()
	e.Float(1.0)
	if got := hexOf(t, e.Bytes()); got != "0000803f" {
		t.Errorf("Float(1.0) = %s, spec requires 0000803f (little-endian)", got)
	}
	e = NewEncoder()
	e.Double(-2.0)
	if got := hexOf(t, e.Bytes()); got != "00000000000000c0" {
		t.Errorf("Double(-2.0) = %s, want 00000000000000c0", got)
	}
}

func TestOptionalStringIsAUnionBranchIndex(t *testing.T) {
	t.Parallel()
	// Avro spec: a union is encoded as a long giving the zero-based branch
	// index, then the value. For ["null","string"] that is index 0 (encoded 00)
	// and index 1 (encoded ZIGZAG(1) = 02) -- note the branch index goes through
	// zig-zag too, so a literal 0x01 byte here would be wrong (it decodes as -1).
	e := NewEncoder()
	e.OptionalString("", false)
	if got := hexOf(t, e.Bytes()); got != "00" {
		t.Errorf("OptionalString(null) = %s, spec requires 00 (branch 0)", got)
	}
	e = NewEncoder()
	e.OptionalString("hi", true)
	if got := hexOf(t, e.Bytes()); got != "02046869" {
		t.Errorf("OptionalString(%q) = %s, spec requires 02046869 (branch 1, then \"hi\")", "hi", got)
	}

	// A present-but-empty string must stay distinguishable from null: the whole
	// point of the union. Losing that distinction is silent data loss that a
	// naive "did it round-trip" check on non-empty values never sees.
	e = NewEncoder()
	e.OptionalString("", true)
	if got := hexOf(t, e.Bytes()); got != "0200" {
		t.Errorf("OptionalString(\"\", present) = %s, want 0200; must differ from null (00)", got)
	}
	s, present, err := NewDecoder(bytes.NewReader([]byte{0x02, 0x00})).OptionalString()
	if err != nil {
		t.Fatalf("OptionalString decode: %v", err)
	}
	if !present || s != "" {
		t.Errorf("decoded present-but-empty union as (%q, present=%v), want (\"\", true)", s, present)
	}
	s, present, err = NewDecoder(bytes.NewReader([]byte{0x00})).OptionalString()
	if err != nil {
		t.Fatalf("OptionalString decode: %v", err)
	}
	if present || s != "" {
		t.Errorf("decoded null union as (%q, present=%v), want (\"\", false)", s, present)
	}
}

func TestFloat64ArrayUsesSpecBlockFraming(t *testing.T) {
	t.Parallel()
	// Avro spec: arrays are "a series of blocks", each a long item-count
	// followed by the items, terminated by a block with count 0. An empty array
	// is just the zero terminator. Forgetting the terminator, or emitting a
	// count for an empty array, produces a file that reads as truncated
	// elsewhere -- and is invisible to a round-trip test against this decoder.
	e := NewEncoder()
	e.Float64Array(nil)
	if got := hexOf(t, e.Bytes()); got != "00" {
		t.Errorf("Float64Array(nil) = %s, spec requires just the 00 terminator", got)
	}
	e = NewEncoder()
	e.Float64Array([]float64{1.0})
	// count 1 (02), the double, terminator (00)
	if got := hexOf(t, e.Bytes()); got != "02"+"000000000000f03f"+"00" {
		t.Errorf("Float64Array([1.0]) = %s, want 02000000000000f03f00", got)
	}

	e = NewEncoder()
	e.Float64Matrix([][]float64{{1.0}, {}})
	// outer count 2 (04), inner [1.0] (02 <double> 00), inner [] (00), outer terminator (00)
	want := "04" + "02" + "000000000000f03f" + "00" + "00" + "00"
	if got := hexOf(t, e.Bytes()); got != want {
		t.Errorf("Float64Matrix([[1.0],[]]) = %s, want %s", got, want)
	}
}

func TestDecoderReadsSpecNegativeBlockCounts(t *testing.T) {
	t.Parallel()
	// Avro spec: "If a block's count is negative, its absolute value is used,
	// and the count is followed by a long block size". Other writers (the Java
	// SDK does this when it knows the byte size) use this form. store claims its
	// files are readable by, and can read from, any spec-conformant Avro writer
	// -- so the decoder has to accept it even though our encoder never emits it.
	var b bytes.Buffer
	e := NewEncoder()
	e.Long(-2)  // 2 items, size-prefixed form
	e.Long(16)  // block byte size
	e.Double(1) //
	e.Double(2) //
	e.Long(0)   // terminator
	b.Write(e.Bytes())

	got, err := NewDecoder(&b).Float64Array()
	if err != nil {
		t.Fatalf("Float64Array on spec size-prefixed block: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("size-prefixed array block decoded as %v, want [1 2]", got)
	}
}

func TestDecoderRejectsNegativeLengths(t *testing.T) {
	t.Parallel()
	// A corrupt or hostile length prefix must produce an error, never a panic
	// and never a silently-empty read. make([]byte, negative) panics, so this is
	// the difference between "the backend returns an error for a corrupt object"
	// and "the backend dies".
	//
	// -1 zig-zag encodes as 0x01.
	_, err := NewDecoder(bytes.NewReader([]byte{0x01})).Blob()
	if err == nil {
		t.Fatal("Blob() accepted a negative length prefix; a corrupt object must error")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("Blob() negative-length error = %q, want it to name the negative length", err)
	}
}

func TestDecoderRejectsVarintOverflow(t *testing.T) {
	t.Parallel()
	// An unterminated varint must not spin forever or silently truncate.
	junk := bytes.Repeat([]byte{0xff}, 32)
	_, err := NewDecoder(bytes.NewReader(junk)).Long()
	if err == nil {
		t.Fatal("Long() accepted a 32-byte varint; must reject overflow")
	}
	if !strings.Contains(err.Error(), "overflow") {
		t.Errorf("overflow error = %q, want it to say overflow", err)
	}
}

func TestDecoderReportsTruncationRatherThanReturningZero(t *testing.T) {
	t.Parallel()
	// The dominant silent-failure shape: a short read that returns a zero value
	// and no error, so a truncated object reads back as "0.0" or "" instead of
	// blowing up. Every fixed-width and length-prefixed reader must error.
	if _, err := NewDecoder(bytes.NewReader([]byte{0x01, 0x02, 0x03})).Double(); err == nil {
		t.Error("Double() on 3 bytes returned no error; truncation must not read as 0.0")
	}
	if _, err := NewDecoder(bytes.NewReader([]byte{0x01})).Float(); err == nil {
		t.Error("Float() on 1 byte returned no error; truncation must not read as 0.0")
	}
	// length says 10 bytes, only 3 present
	if _, err := NewDecoder(bytes.NewReader([]byte{0x14, 'a', 'b', 'c'})).String(); err == nil {
		t.Error("String() with a length prefix past EOF returned no error")
	}
	if _, err := NewDecoder(bytes.NewReader(nil)).Bool(); err == nil {
		t.Error("Bool() at EOF returned no error; must not read as false")
	}
}

func TestRoundTripPreservesAdversarialValues(t *testing.T) {
	t.Parallel()
	// Values that a plausible-but-wrong implementation mangles: float32 written
	// through a float64 path, NaN/Inf, subnormals, negative zero, embedded NULs
	// and non-UTF-8 in strings, and int64 extremes.
	type rec struct {
		l    int64
		i    int32
		f    float32
		d    float64
		s    string
		blob []byte
		b    bool
	}
	recs := []rec{
		{math.MinInt64, math.MinInt32, float32(math.Inf(-1)), math.Inf(1), "", nil, false},
		{math.MaxInt64, math.MaxInt32, 3.4028235e38, 5e-324, "a\x00b", []byte{0, 0, 0}, true},
		{-1, -1, -0.0, math.Copysign(0, -1), "héllo \U0001F600", []byte{0xff, 0xfe}, true},
		{0, 0, 1.0 / 3.0, 1.0 / 3.0, "\xff\xfe not utf8", []byte{}, false},
	}
	e := NewEncoder()
	for _, r := range recs {
		e.Long(r.l)
		e.Int(r.i)
		e.Float(r.f)
		e.Double(r.d)
		e.String(r.s)
		e.Blob(r.blob)
		e.Bool(r.b)
	}
	d := NewDecoder(bytes.NewReader(e.Bytes()))
	for i, want := range recs {
		var got rec
		var err error
		if got.l, err = d.Long(); err != nil {
			t.Fatalf("rec %d Long: %v", i, err)
		}
		if got.i, err = d.Int(); err != nil {
			t.Fatalf("rec %d Int: %v", i, err)
		}
		if got.f, err = d.Float(); err != nil {
			t.Fatalf("rec %d Float: %v", i, err)
		}
		if got.d, err = d.Double(); err != nil {
			t.Fatalf("rec %d Double: %v", i, err)
		}
		if got.s, err = d.String(); err != nil {
			t.Fatalf("rec %d String: %v", i, err)
		}
		if got.blob, err = d.Blob(); err != nil {
			t.Fatalf("rec %d Blob: %v", i, err)
		}
		if got.b, err = d.Bool(); err != nil {
			t.Fatalf("rec %d Bool: %v", i, err)
		}

		if got.l != want.l || got.i != want.i {
			t.Errorf("rec %d integers: got (%d,%d) want (%d,%d)", i, got.l, got.i, want.l, want.i)
		}
		// Compare float bit patterns so NaN, -0.0 and Inf are all exact.
		if math.Float32bits(got.f) != math.Float32bits(want.f) {
			t.Errorf("rec %d float32: got %v (%#x) want %v (%#x)", i, got.f, math.Float32bits(got.f), want.f, math.Float32bits(want.f))
		}
		if math.Float64bits(got.d) != math.Float64bits(want.d) {
			t.Errorf("rec %d float64: got %v (%#x) want %v (%#x)", i, got.d, math.Float64bits(got.d), want.d, math.Float64bits(want.d))
		}
		if got.s != want.s {
			t.Errorf("rec %d string: got %q want %q", i, got.s, want.s)
		}
		if !bytes.Equal(got.blob, want.blob) {
			t.Errorf("rec %d blob: got %v want %v", i, got.blob, want.blob)
		}
		if got.b != want.b {
			t.Errorf("rec %d bool: got %v want %v", i, got.b, want.b)
		}
	}
}

func TestFloat32IsNotWidenedThroughFloat64(t *testing.T) {
	t.Parallel()
	// Avro float is 4 bytes. Writing it via the double path would produce 8
	// bytes and shift every subsequent field in the record -- a corruption that
	// only shows up when a float is followed by another field.
	e := NewEncoder()
	e.Float(1.5)
	e.String("sentinel")
	d := NewDecoder(bytes.NewReader(e.Bytes()))
	if _, err := d.Float(); err != nil {
		t.Fatalf("Float: %v", err)
	}
	s, err := d.String()
	if err != nil {
		t.Fatalf("String after Float: %v", err)
	}
	if s != "sentinel" {
		t.Errorf("field after a float decoded as %q, want %q -- the float consumed the wrong number of bytes", s, "sentinel")
	}
}

func TestEncoderResetClearsPreviousRecord(t *testing.T) {
	t.Parallel()
	// Encoders are reused across records in WriteOCF-adjacent code; a Reset that
	// does not actually clear would silently prepend the previous record.
	e := NewEncoder()
	e.String("first")
	e.Reset()
	e.String("second")
	got, err := NewDecoder(bytes.NewReader(e.Bytes())).String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if got != "second" {
		t.Errorf("after Reset the buffer decoded as %q, want %q", got, "second")
	}
	if n := len(e.Bytes()); n != 7 {
		t.Errorf("after Reset the buffer is %d bytes, want 7 (1 length + 6 payload)", n)
	}
}
