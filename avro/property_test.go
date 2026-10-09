package avro

import (
	"bytes"
	"math"
	"math/rand/v2"
	"testing"
	"testing/quick"
)

// A codec has exactly one property worth stating, and it covers every field
// type at once: whatever goes in comes back. Stated with testing/quick and
// generated values rather than a table, because the encodings that go wrong
// are the boundaries -- zigzag at the sign flip, varint at each 7-bit step,
// NaN and the signed zeroes -- and a table lists the ones its author thought
// of.

var qcfg = &quick.Config{MaxCount: 5000}

func TestPropertyLongRoundTrips(t *testing.T) {
	f := func(v int64) bool {
		e := NewEncoder()
		e.Long(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Long()
		return err == nil && got == v
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
}

// The varint boundaries explicitly: one byte either side of every 7-bit step,
// and both ends of the range. Generated int64s almost never land on these.
func TestPropertyLongRoundTripsAtEveryVarintBoundary(t *testing.T) {
	var vals []int64
	for shift := 0; shift < 64; shift++ {
		b := int64(1) << uint(shift)
		vals = append(vals, b-1, b, b+1, -b-1, -b, -b+1)
	}
	vals = append(vals, 0, 1, -1, math.MaxInt64, math.MinInt64, math.MaxInt64-1, math.MinInt64+1)
	for _, v := range vals {
		e := NewEncoder()
		e.Long(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Long()
		if err != nil || got != v {
			t.Fatalf("Long(%d) came back as %d (%v)", v, got, err)
		}
	}
}

func TestPropertyIntRoundTrips(t *testing.T) {
	f := func(v int32) bool {
		e := NewEncoder()
		e.Int(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Int()
		return err == nil && got == v
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
}

func TestPropertyStringAndBlobRoundTrip(t *testing.T) {
	f := func(s string, b []byte) bool {
		e := NewEncoder()
		e.String(s)
		e.Blob(b)
		d := NewDecoder(bytes.NewReader(e.Bytes()))
		gotS, err := d.String()
		if err != nil || gotS != s {
			return false
		}
		gotB, err := d.Blob()
		if err != nil {
			return false
		}
		// A nil and an empty slice are the same empty blob on the wire.
		return bytes.Equal(gotB, b)
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
}

// Doubles must round-trip BIT FOR BIT, including NaN and the two zeroes. A
// codec that normalised -0 to 0, or lost a NaN payload, would change data it
// was only asked to carry.
func TestPropertyDoubleRoundTripsBitForBit(t *testing.T) {
	f := func(bits uint64) bool {
		v := math.Float64frombits(bits)
		e := NewEncoder()
		e.Double(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Double()
		return err == nil && math.Float64bits(got) == bits
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
	for _, v := range []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.NaN(),
		math.SmallestNonzeroFloat64, math.MaxFloat64} {
		e := NewEncoder()
		e.Double(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Double()
		if err != nil || math.Float64bits(got) != math.Float64bits(v) {
			t.Fatalf("Double(%v) came back as %v (%v)", v, got, err)
		}
	}
}

func TestPropertyFloatRoundTripsBitForBit(t *testing.T) {
	f := func(bits uint32) bool {
		v := math.Float32frombits(bits)
		e := NewEncoder()
		e.Float(v)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Float()
		return err == nil && math.Float32bits(got) == bits
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
}

// An absent optional string must come back absent, and a present empty string
// must come back present and empty. Collapsing those two is how a field that
// was deliberately left blank becomes a field that was never set.
func TestPropertyOptionalStringKeepsPresence(t *testing.T) {
	f := func(s string, present bool) bool {
		e := NewEncoder()
		e.OptionalString(s, present)
		gotS, gotP, err := NewDecoder(bytes.NewReader(e.Bytes())).OptionalString()
		if err != nil || gotP != present {
			return false
		}
		return !present || gotS == s
	}
	if err := quick.Check(f, qcfg); err != nil {
		t.Fatal(err)
	}
	e := NewEncoder()
	e.OptionalString("", true)
	s, present, err := NewDecoder(bytes.NewReader(e.Bytes())).OptionalString()
	if err != nil || !present || s != "" {
		t.Fatalf("a present empty string came back %q, present=%v (%v)", s, present, err)
	}
}

func TestPropertyFloat64ArrayAndMatrixRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(161, 162))
	for i := 0; i < 3000; i++ {
		xs := make([]float64, r.IntN(40))
		for j := range xs {
			xs[j] = r.NormFloat64() * math.Pow(10, float64(r.IntN(40)-20))
		}
		m := make([][]float64, r.IntN(8))
		for j := range m {
			m[j] = make([]float64, r.IntN(8))
			for k := range m[j] {
				m[j][k] = r.NormFloat64()
			}
		}
		e := NewEncoder()
		e.Float64Array(xs)
		e.Float64Matrix(m)
		d := NewDecoder(bytes.NewReader(e.Bytes()))

		gotXS, err := d.Float64Array()
		if err != nil {
			t.Fatalf("array: %v", err)
		}
		if len(gotXS) != len(xs) {
			t.Fatalf("array length %d, want %d", len(gotXS), len(xs))
		}
		for j := range xs {
			if math.Float64bits(gotXS[j]) != math.Float64bits(xs[j]) {
				t.Fatalf("array[%d] = %v, want %v", j, gotXS[j], xs[j])
			}
		}

		gotM, err := d.Float64Matrix()
		if err != nil {
			t.Fatalf("matrix: %v", err)
		}
		if len(gotM) != len(m) {
			t.Fatalf("matrix rows %d, want %d", len(gotM), len(m))
		}
		for j := range m {
			if len(gotM[j]) != len(m[j]) {
				t.Fatalf("matrix row %d has %d values, want %d", j, len(gotM[j]), len(m[j]))
			}
			for k := range m[j] {
				if gotM[j][k] != m[j][k] {
					t.Fatalf("matrix[%d][%d] = %v, want %v", j, k, gotM[j][k], m[j][k])
				}
			}
		}
	}
}

// A stream of mixed fields round-trips in order. Each type is correct on its
// own above; this is the property that they do not disturb each other, which
// is where a missing length prefix or an over-read shows up.
func TestPropertyMixedStreamsRoundTripInOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(163, 164))
	for i := 0; i < 2000; i++ {
		n := 1 + r.IntN(30)
		kinds := make([]int, n)
		longs := make([]int64, n)
		strs := make([]string, n)
		bools := make([]bool, n)
		dbls := make([]float64, n)

		e := NewEncoder()
		for j := 0; j < n; j++ {
			kinds[j] = r.IntN(4)
			switch kinds[j] {
			case 0:
				longs[j] = int64(r.Uint64())
				e.Long(longs[j])
			case 1:
				b := make([]byte, r.IntN(20))
				for k := range b {
					b[k] = byte(r.IntN(256))
				}
				strs[j] = string(b)
				e.String(strs[j])
			case 2:
				bools[j] = r.IntN(2) == 0
				e.Bool(bools[j])
			default:
				dbls[j] = r.NormFloat64()
				e.Double(dbls[j])
			}
		}

		d := NewDecoder(bytes.NewReader(e.Bytes()))
		for j := 0; j < n; j++ {
			switch kinds[j] {
			case 0:
				got, err := d.Long()
				if err != nil || got != longs[j] {
					t.Fatalf("field %d: Long = %v (%v), want %v", j, got, err, longs[j])
				}
			case 1:
				got, err := d.String()
				if err != nil || got != strs[j] {
					t.Fatalf("field %d: String = %q (%v), want %q", j, got, err, strs[j])
				}
			case 2:
				got, err := d.Bool()
				if err != nil || got != bools[j] {
					t.Fatalf("field %d: Bool = %v (%v), want %v", j, got, err, bools[j])
				}
			default:
				got, err := d.Double()
				if err != nil || got != dbls[j] {
					t.Fatalf("field %d: Double = %v (%v), want %v", j, got, err, dbls[j])
				}
			}
		}
	}
}

// A truncated stream must return an error, never a value. A decoder that read
// past the end of its input and reported success would turn a cut-off file
// into silently wrong records.
func TestPropertyTruncatedInputAlwaysErrors(t *testing.T) {
	r := rand.New(rand.NewPCG(165, 166))
	for i := 0; i < 3000; i++ {
		e := NewEncoder()
		s := make([]byte, 1+r.IntN(30))
		for j := range s {
			s[j] = byte('a' + r.IntN(26))
		}
		e.String(string(s))
		e.Long(int64(r.Uint64()))
		full := e.Bytes()
		cut := r.IntN(len(full)) // strictly shorter than the whole

		d := NewDecoder(bytes.NewReader(full[:cut]))
		_, err1 := d.String()
		_, err2 := d.Long()
		if err1 == nil && err2 == nil {
			t.Fatalf("a %d-byte prefix of %d decoded cleanly", cut, len(full))
		}
	}
}
