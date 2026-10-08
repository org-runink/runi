package avro

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

var errDisk = errors.New("disk full")

// failWriter accepts n bytes and then fails, so a sweep over n puts the failure
// at every point in the output in turn.
type failWriter struct{ left int }

func (w *failWriter) Write(p []byte) (int, error) {
	if len(p) <= w.left {
		w.left -= len(p)
		return len(p), nil
	}
	n := w.left
	w.left = 0
	return n, errDisk
}

type rec struct {
	Name string
	Vals []float64
	Mat  [][]float64
	Raw  []byte
}

func marshalRec(e *Encoder, v rec) {
	e.String(v.Name)
	e.Float64Array(v.Vals)
	e.Float64Matrix(v.Mat)
	e.Blob(v.Raw)
}

func unmarshalRec(d *Decoder) (rec, error) {
	var v rec
	var err error
	if v.Name, err = d.String(); err != nil {
		return v, err
	}
	if v.Vals, err = d.Float64Array(); err != nil {
		return v, err
	}
	if v.Mat, err = d.Float64Matrix(); err != nil {
		return v, err
	}
	if v.Raw, err = d.Blob(); err != nil {
		return v, err
	}
	return v, nil
}

func sample() []rec {
	return []rec{
		{Name: "alpha", Vals: []float64{1, 2, 3}, Mat: [][]float64{{1, 2}, {3, 4}}, Raw: []byte("xyz")},
		{Name: "beta", Vals: []float64{4.5}, Mat: [][]float64{{9}}, Raw: []byte{0, 1, 2}},
	}
}

const schema = `{"type":"record","name":"R","fields":[]}`

// Every write in WriteOCF can fail, because every one of them is a real write
// to someone's disk or socket. Failing at each byte offset in turn walks all of
// them; none may be swallowed and none may panic.
func TestWriteOCFPropagatesEveryWriteError(t *testing.T) {
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, "null", sample(), marshalRec); err != nil {
		t.Fatalf("baseline write: %v", err)
	}
	total := full.Len()
	if total < 16 {
		t.Fatalf("suspiciously small baseline (%d bytes): the sweep would prove nothing", total)
	}
	failed := 0
	for n := 0; n < total; n++ {
		err := WriteOCF(&failWriter{left: n}, schema, "null", sample(), marshalRec)
		if err == nil {
			t.Fatalf("writer failed after %d of %d bytes but WriteOCF reported success", n, total)
		}
		failed++
	}
	if failed != total {
		t.Fatalf("swept %d offsets, expected %d", failed, total)
	}
	// And with the whole file accepted it still succeeds.
	if err := WriteOCF(&failWriter{left: total}, schema, "null", sample(), marshalRec); err != nil {
		t.Errorf("full-length writer: %v", err)
	}
}

// The same sweep for reading: a file truncated at any point must come back as
// an error, never as a short but plausible record list.
func TestReadOCFRejectsEveryTruncation(t *testing.T) {
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, "null", sample(), marshalRec); err != nil {
		t.Fatalf("baseline write: %v", err)
	}
	b := full.Bytes()
	for n := 0; n < len(b); n++ {
		_, got, err := ReadOCF(bytes.NewReader(b[:n]), unmarshalRec)
		if err == nil && len(got) == len(sample()) {
			t.Fatalf("truncated to %d of %d bytes but ReadOCF returned a full result",
				n, len(b))
		}
	}
	hdr, got, err := ReadOCF(bytes.NewReader(b), unmarshalRec)
	if err != nil {
		t.Fatalf("full read: %v", err)
	}
	if len(got) != 2 || hdr.Codec != "null" {
		t.Fatalf("round trip: %d records, codec %q", len(got), hdr.Codec)
	}
}

func TestWriteOCFDeflatePropagatesWriteErrors(t *testing.T) {
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, "deflate", sample(), marshalRec); err != nil {
		t.Fatalf("baseline deflate write: %v", err)
	}
	for n := 0; n < full.Len(); n++ {
		if err := WriteOCF(&failWriter{left: n}, schema, "deflate", sample(), marshalRec); err == nil {
			t.Fatalf("deflate: writer failed after %d bytes but WriteOCF succeeded", n)
		}
	}
}

// A decoder reading a value whose length prefix outruns the data must say so
// rather than returning what it managed to read.
func TestDecoderTruncatedValues(t *testing.T) {
	e := NewEncoder()
	e.Blob(bytes.Repeat([]byte("q"), 40))
	blob := e.Bytes()
	for n := 0; n < len(blob); n++ {
		if _, err := NewDecoder(bytes.NewReader(blob[:n])).Blob(); err == nil {
			t.Errorf("Blob truncated to %d of %d bytes decoded without error", n, len(blob))
		}
	}

	e.Reset()
	e.Float64Array([]float64{1, 2, 3, 4, 5})
	arr := e.Bytes()
	for n := 0; n < len(arr); n++ {
		if _, err := NewDecoder(bytes.NewReader(arr[:n])).Float64Array(); err == nil {
			t.Errorf("Float64Array truncated to %d of %d bytes decoded without error", n, len(arr))
		}
	}

	e.Reset()
	e.Float64Matrix([][]float64{{1, 2}, {3, 4}, {5, 6}})
	mat := e.Bytes()
	for n := 0; n < len(mat); n++ {
		if _, err := NewDecoder(bytes.NewReader(mat[:n])).Float64Matrix(); err == nil {
			t.Errorf("Float64Matrix truncated to %d of %d bytes decoded without error", n, len(mat))
		}
	}
}

// A negative length is the corrupt-input case that must not become an
// allocation.
func TestDecoderNegativeLength(t *testing.T) {
	e := NewEncoder()
	e.Long(-8) // a length prefix that cannot be one
	if _, err := NewDecoder(bytes.NewReader(e.Bytes())).Blob(); err == nil {
		t.Error("negative length decoded without error")
	}
}

var _ io.Writer = (*failWriter)(nil)

// errReader returns n good bytes and then a real error, which is different
// from running out of input: a short read is an honest end, an error is a
// broken pipe or a bad disk, and the two must not be confused.
type errReader struct {
	data []byte
	n    int
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, errDisk
	}
	k := copy(p, r.data[:min(len(r.data), min(len(p), r.n))])
	r.data = r.data[k:]
	r.n -= k
	return k, nil
}

func TestReadOCFPropagatesReadErrors(t *testing.T) {
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, "null", sample(), marshalRec); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	b := full.Bytes()
	for n := 0; n < len(b); n++ {
		if _, _, err := ReadOCF(&errReader{data: append([]byte(nil), b...), n: n}, unmarshalRec); err == nil {
			t.Fatalf("reader failed after %d of %d bytes but ReadOCF succeeded", n, len(b))
		}
	}
}

func TestDecoderPropagatesReadErrors(t *testing.T) {
	e := NewEncoder()
	e.Blob(bytes.Repeat([]byte("z"), 30))
	b := e.Bytes()
	for n := 0; n < len(b); n++ {
		if _, err := NewDecoder(&errReader{data: append([]byte(nil), b...), n: n}).Blob(); err == nil {
			t.Errorf("Blob: reader failed after %d bytes but decoding succeeded", n)
		}
	}
}

// A spec-conformant writer may emit an array as a negative count followed by
// the block's byte size. Both numbers come from the file, so a reader that
// stops mid-size must report it rather than treat the block as empty.
func TestArrayBlockWithSizeTruncated(t *testing.T) {
	// -2 items, then the byte size, then the items, then a 0 terminator.
	inner := NewEncoder()
	inner.Double(1.5)
	inner.Double(2.5)
	items := inner.Bytes()

	e := NewEncoder()
	e.Long(-2)
	e.Long(int64(len(items)))
	full := append(append([]byte(nil), e.Bytes()...), items...)
	term := NewEncoder()
	term.Long(0)
	full = append(full, term.Bytes()...)

	if got, err := NewDecoder(bytes.NewReader(full)).Float64Array(); err != nil || len(got) != 2 {
		t.Fatalf("block-with-size array: %v %v", got, err)
	}
	// Cutting it anywhere inside must not decode cleanly.
	for n := 0; n < len(full); n++ {
		if _, err := NewDecoder(bytes.NewReader(full[:n])).Float64Array(); err == nil {
			t.Errorf("truncated to %d of %d decoded without error", n, len(full))
		}
	}
}

func TestMatrixBlockWithSizeTruncated(t *testing.T) {
	row := NewEncoder()
	row.Float64Array([]float64{1, 2})
	row.Float64Array([]float64{3, 4})
	rows := row.Bytes()

	e := NewEncoder()
	e.Long(-2)
	e.Long(int64(len(rows)))
	full := append(append([]byte(nil), e.Bytes()...), rows...)
	term := NewEncoder()
	term.Long(0)
	full = append(full, term.Bytes()...)

	if got, err := NewDecoder(bytes.NewReader(full)).Float64Matrix(); err != nil || len(got) != 2 {
		t.Fatalf("block-with-size matrix: %v %v", got, err)
	}
	for n := 0; n < len(full); n++ {
		if _, err := NewDecoder(bytes.NewReader(full[:n])).Float64Matrix(); err == nil {
			t.Errorf("truncated to %d of %d decoded without error", n, len(full))
		}
	}
}

// If the sync marker cannot be generated the file must not be written: a
// container whose marker is absent cannot be read back.
func TestWriteOCFSyncMarkerFailure(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errDisk }
	defer func() { randRead = orig }()

	var buf bytes.Buffer
	if err := WriteOCF(&buf, schema, "null", sample(), marshalRec); !errors.Is(err, errDisk) {
		t.Fatalf("err = %v, want errDisk", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes despite failing to make a sync marker", buf.Len())
	}
}
