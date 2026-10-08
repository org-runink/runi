// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

// Round trips and failure modes for the decoder helpers, including the
// block-with-size encoding that a writer may emit instead of a plain count.

func TestBlobRoundTrip(t *testing.T) {
	for _, in := range [][]byte{nil, {}, []byte("x"), bytes.Repeat([]byte("ab"), 5000)} {
		e := NewEncoder()
		e.Blob(in)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Blob()
		if err != nil {
			t.Fatalf("Blob(%d bytes): %v", len(in), err)
		}
		if !bytes.Equal(got, in) && !(len(got) == 0 && len(in) == 0) {
			t.Fatalf("Blob round trip: got %d bytes, want %d", len(got), len(in))
		}
	}
}

func TestBlobRejectsNegativeLength(t *testing.T) {
	// A corrupt length must be refused, not used to size an allocation.
	e := NewEncoder()
	e.Long(-5)
	_, err := NewDecoder(bytes.NewReader(e.Bytes())).Blob()
	if err == nil || !strings.Contains(err.Error(), "negative length") {
		t.Fatalf("Blob with a negative length err = %v; want a negative-length error", err)
	}
}

func TestBlobTruncatedInput(t *testing.T) {
	// A length longer than the bytes present must error rather than allocate it.
	e := NewEncoder()
	e.Long(1 << 20) // claims a megabyte
	if _, err := NewDecoder(bytes.NewReader(e.Bytes())).Blob(); err == nil {
		t.Fatal("Blob accepted a length far beyond the available input")
	}
	// And a header with no length at all.
	if _, err := NewDecoder(bytes.NewReader(nil)).Blob(); !errors.Is(err, io.EOF) && err == nil {
		t.Fatal("Blob on empty input should error")
	}
}

func TestOptionalString(t *testing.T) {
	e := NewEncoder()
	e.OptionalString("hello", true)
	s, present, err := NewDecoder(bytes.NewReader(e.Bytes())).OptionalString()
	if err != nil || !present || s != "hello" {
		t.Fatalf("present OptionalString = %q,%v,%v", s, present, err)
	}

	e.Reset()
	e.OptionalString("ignored", false)
	s, present, err = NewDecoder(bytes.NewReader(e.Bytes())).OptionalString()
	if err != nil || present || s != "" {
		t.Fatalf("absent OptionalString = %q,%v,%v; want \"\",false,nil", s, present, err)
	}

	if _, _, err := NewDecoder(bytes.NewReader(nil)).OptionalString(); err == nil {
		t.Fatal("OptionalString on empty input should error")
	}
}

func TestFloat64ArrayRoundTrip(t *testing.T) {
	for _, in := range [][]float64{
		nil,
		{1.5},
		{0, -0, 1e308, -1e308, math.SmallestNonzeroFloat64},
		make([]float64, 1000),
	} {
		e := NewEncoder()
		e.Float64Array(in)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Array()
		if err != nil {
			t.Fatalf("Float64Array(%d): %v", len(in), err)
		}
		if len(got) != len(in) {
			t.Fatalf("length = %d; want %d", len(got), len(in))
		}
		for i := range in {
			if got[i] != in[i] {
				t.Fatalf("[%d] = %v; want %v", i, got[i], in[i])
			}
		}
	}
}

func TestFloat64ArrayBlockWithSizeForm(t *testing.T) {
	// A writer may emit a NEGATIVE count followed by a byte size. Readers must
	// accept it; this package's own encoder does not produce it, so without
	// this test that branch is never exercised.
	e := NewEncoder()
	e.Long(-2)  // two items, block-with-size form
	e.Long(16)  // the byte size, which the reader skips
	e.Double(1) //
	e.Double(2) //
	e.Long(0)   // terminator
	got, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Array()
	if err != nil {
		t.Fatalf("block-with-size Float64Array: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v; want [1 2]", got)
	}
}

func TestFloat64ArrayTruncated(t *testing.T) {
	e := NewEncoder()
	e.Long(3) // promises three doubles
	e.Double(1)
	if _, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Array(); err == nil {
		t.Fatal("Float64Array accepted a block shorter than its count")
	}
	if _, err := NewDecoder(bytes.NewReader(nil)).Float64Array(); err == nil {
		t.Fatal("Float64Array on empty input should error")
	}
}

func TestFloat64MatrixRoundTrip(t *testing.T) {
	for _, in := range [][][]float64{
		nil,
		{{1, 2, 3}},
		{{1}, {2, 3}, {}},
		{{1.5, -2.5}, {0, 1e-300}},
	} {
		e := NewEncoder()
		e.Float64Matrix(in)
		got, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Matrix()
		if err != nil {
			t.Fatalf("Float64Matrix(%d rows): %v", len(in), err)
		}
		if len(got) != len(in) {
			t.Fatalf("rows = %d; want %d", len(got), len(in))
		}
		for i := range in {
			if len(got[i]) != len(in[i]) {
				t.Fatalf("row %d length = %d; want %d", i, len(got[i]), len(in[i]))
			}
			for j := range in[i] {
				if got[i][j] != in[i][j] {
					t.Fatalf("[%d][%d] = %v; want %v", i, j, got[i][j], in[i][j])
				}
			}
		}
	}
}

func TestFloat64MatrixBlockWithSizeForm(t *testing.T) {
	e := NewEncoder()
	e.Long(-1) // one row, block-with-size form
	e.Long(8)  // byte size, skipped
	e.Float64Array([]float64{7, 8})
	e.Long(0) // terminator
	got, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Matrix()
	if err != nil {
		t.Fatalf("block-with-size Float64Matrix: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != 7 || got[0][1] != 8 {
		t.Fatalf("got %v; want [[7 8]]", got)
	}
}

func TestFloat64MatrixTruncated(t *testing.T) {
	e := NewEncoder()
	e.Long(2) // promises two rows
	e.Float64Array([]float64{1})
	if _, err := NewDecoder(bytes.NewReader(e.Bytes())).Float64Matrix(); err == nil {
		t.Fatal("Float64Matrix accepted fewer rows than its count")
	}
	if _, err := NewDecoder(bytes.NewReader(nil)).Float64Matrix(); err == nil {
		t.Fatal("Float64Matrix on empty input should error")
	}
}

func TestWriteOCFRejectsAnUnknownCodec(t *testing.T) {
	var buf bytes.Buffer
	err := WriteOCF(&buf, `{"type":"record","name":"R","fields":[]}`, "snappy",
		[]int{1}, func(e *Encoder, v int) { e.Long(int64(v)) })
	if err == nil {
		t.Fatal("WriteOCF accepted an unsupported codec")
	}
	if buf.Len() != 0 {
		t.Fatalf("WriteOCF wrote %d bytes before refusing the codec", buf.Len())
	}
}

func TestReadOCFRejectsNonOCFInput(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte("not an avro file"),
		{'O', 'b', 'j'},       // truncated magic
		{'O', 'b', 'j', 0x09}, // wrong version byte
	} {
		_, _, err := ReadOCF(bytes.NewReader(in), func(d *Decoder) (int, error) {
			v, err := d.Long()
			return int(v), err
		})
		if err == nil {
			t.Fatalf("ReadOCF accepted %q as a container file", in)
		}
	}
}

func TestWriteReadOCFBothCodecs(t *testing.T) {
	schema := `{"type":"record","name":"N","fields":[{"name":"v","type":"long"}]}`
	in := []int64{1, 2, 3, 4, 5}
	for _, codec := range []string{CodecNull, CodecDeflate} {
		var buf bytes.Buffer
		if err := WriteOCF(&buf, schema, codec, in,
			func(e *Encoder, v int64) { e.Long(v) }); err != nil {
			t.Fatalf("WriteOCF(%s): %v", codec, err)
		}
		hdr, got, err := ReadOCF(bytes.NewReader(buf.Bytes()), func(d *Decoder) (int64, error) {
			return d.Long()
		})
		if err != nil {
			t.Fatalf("ReadOCF(%s): %v", codec, err)
		}
		if hdr.Codec != codec {
			t.Fatalf("header codec = %q; want %q", hdr.Codec, codec)
		}
		if len(got) != len(in) {
			t.Fatalf("%s: got %d records, want %d", codec, len(got), len(in))
		}
		for i := range in {
			if got[i] != in[i] {
				t.Fatalf("%s: [%d] = %d; want %d", codec, i, got[i], in[i])
			}
		}
	}
}
