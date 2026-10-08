// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"errors"
	"testing"
)

// A writer that fails after n bytes, so each write in WriteOCF's sequence can
// be made to fail in turn. Without this the error branches are unreachable:
// a bytes.Buffer never fails.
type failAfter struct {
	n    int
	seen int
}

var errWriter = errors.New("writer failed")

func (f *failAfter) Write(p []byte) (int, error) {
	if f.seen+len(p) > f.n {
		allowed := f.n - f.seen
		if allowed < 0 {
			allowed = 0
		}
		f.seen += allowed
		return allowed, errWriter
	}
	f.seen += len(p)
	return len(p), nil
}

func TestWriteOCFSurfacesWriterErrors(t *testing.T) {
	schema := `{"type":"record","name":"N","fields":[{"name":"v","type":"long"}]}`
	recs := []int64{1, 2, 3}
	marshal := func(e *Encoder, v int64) { e.Long(v) }

	// How long is a successful file? Fail at every prefix of it and require an
	// error every time — no partial write may be reported as success.
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, CodecDeflate, recs, marshal); err != nil {
		t.Fatalf("baseline WriteOCF: %v", err)
	}
	total := full.Len()

	failures := 0
	for _, limit := range []int{0, 1, 4, 16, 40, total / 2, total - 1} {
		if limit < 0 {
			continue
		}
		w := &failAfter{n: limit}
		if err := WriteOCF(w, schema, CodecDeflate, recs, marshal); err == nil {
			t.Fatalf("WriteOCF reported success although the writer failed after %d bytes", limit)
		}
		failures++
	}
	if failures == 0 {
		t.Fatal("no failure cases ran")
	}

	// The same for the null codec, whose write sequence differs.
	for _, limit := range []int{0, 8, 32} {
		w := &failAfter{n: limit}
		if err := WriteOCF(w, schema, CodecNull, recs, marshal); err == nil {
			t.Fatalf("null-codec WriteOCF reported success although the writer failed after %d bytes", limit)
		}
	}
}

func TestReadOCFOnTruncatedFiles(t *testing.T) {
	schema := `{"type":"record","name":"N","fields":[{"name":"v","type":"long"}]}`
	var full bytes.Buffer
	if err := WriteOCF(&full, schema, CodecDeflate, []int64{1, 2, 3},
		func(e *Encoder, v int64) { e.Long(v) }); err != nil {
		t.Fatal(err)
	}
	b := full.Bytes()

	// Every truncation of a valid file must error rather than return records
	// silently. A short read that yields a partial answer is the worst outcome.
	for _, cut := range []int{1, 2, 4, 8, 16, 32, len(b) / 2, len(b) - 1} {
		if cut <= 0 || cut >= len(b) {
			continue
		}
		_, got, err := ReadOCF(bytes.NewReader(b[:cut]), func(d *Decoder) (int64, error) {
			return d.Long()
		})
		if err == nil && len(got) == 3 {
			t.Fatalf("ReadOCF returned a complete result from %d of %d bytes", cut, len(b))
		}
	}
}

func TestReadMetaMapRejectsMalformedMetadata(t *testing.T) {
	// The header's metadata map is attacker-controlled in the sense that it
	// comes from the file. Each malformed shape must be refused.
	magic := []byte{'O', 'b', 'j', 1}

	cases := map[string][]byte{
		"count then nothing": func() []byte {
			e := NewEncoder()
			e.Long(1) // promises one pair
			return append(append([]byte{}, magic...), e.Bytes()...)
		}(),
		"key then nothing": func() []byte {
			e := NewEncoder()
			e.Long(1)
			e.String("avro.schema")
			return append(append([]byte{}, magic...), e.Bytes()...)
		}(),
		"negative block, no size": func() []byte {
			e := NewEncoder()
			e.Long(-1)
			return append(append([]byte{}, magic...), e.Bytes()...)
		}(),
		"no terminator": func() []byte {
			e := NewEncoder()
			e.Long(1)
			e.String("avro.schema")
			e.Blob([]byte(`{"type":"record","name":"N","fields":[]}`))
			return append(append([]byte{}, magic...), e.Bytes()...)
		}(),
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ReadOCF(bytes.NewReader(in), func(d *Decoder) (int64, error) {
				return d.Long()
			}); err == nil {
				t.Fatal("ReadOCF accepted malformed header metadata")
			}
		})
	}
}

func TestReadMetaMapAcceptsBlockWithSizeForm(t *testing.T) {
	// The negative-count form is legal in the header map too, and this
	// package's writer does not emit it.
	e := NewEncoder()
	e.Long(-1) // one pair, block-with-size
	e.Long(64) // size, skipped
	e.String("avro.schema")
	e.Blob([]byte(`{"type":"record","name":"N","fields":[{"name":"v","type":"long"}]}`))
	e.Long(0) // terminator
	header := append([]byte{'O', 'b', 'j', 1}, e.Bytes()...)

	// Truncated after the header: the metadata must have parsed, and the
	// failure must come later (no sync marker / no blocks), not from the map.
	_, _, err := ReadOCF(bytes.NewReader(header), func(d *Decoder) (int64, error) {
		return d.Long()
	})
	if err == nil {
		t.Fatal("expected a failure after the header, since no blocks follow")
	}
}
