// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"io"
	"testing"
)

// ReadOCFBytes must read exactly what ReadOCF reads, header and all: it is the
// same parser, given the bytes instead of a reader.
func TestReadOCFBytesMatchesReadOCF(t *testing.T) {
	t.Parallel()
	want := sampleEvents()
	for _, codec := range []string{CodecNull, CodecDeflate} {
		var buf bytes.Buffer
		if err := WriteOCF(&buf, testSchema, codec, want, marshalEv); err != nil {
			t.Fatalf("%s: WriteOCF: %v", codec, err)
		}
		hdrR, viaReader, err := ReadOCF(bytes.NewReader(buf.Bytes()), unmarshalEv)
		if err != nil {
			t.Fatalf("%s: ReadOCF: %v", codec, err)
		}
		hdrB, viaBytes, err := ReadOCFBytes(buf.Bytes(), unmarshalEv)
		if err != nil {
			t.Fatalf("%s: ReadOCFBytes: %v", codec, err)
		}
		if hdrB.Schema != hdrR.Schema || hdrB.Codec != hdrR.Codec {
			t.Errorf("%s: header %+v != %+v", codec, hdrB, hdrR)
		}
		if len(viaBytes) != len(viaReader) {
			t.Fatalf("%s: %d records from bytes, %d from a reader", codec, len(viaBytes), len(viaReader))
		}
		for i := range want {
			if !equalEv(viaBytes[i], want[i]) {
				t.Errorf("%s: record %d = %+v, want %+v", codec, i, viaBytes[i], want[i])
			}
		}
	}
}

// The records it returns must not point into the buffer it was given: the
// whole reason to decode in place is that the caller gets its buffer back, and
// a record that aliased it would change under its owner.
func TestReadOCFBytesDoesNotAliasItsInput(t *testing.T) {
	t.Parallel()
	want := sampleEvents()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecNull, want, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	b := buf.Bytes()
	_, got, err := ReadOCFBytes(b, unmarshalEv)
	if err != nil {
		t.Fatalf("ReadOCFBytes: %v", err)
	}
	for i := range b { // the caller reuses its buffer
		b[i] = 0xff
	}
	for i := range want {
		if !equalEv(got[i], want[i]) {
			t.Fatalf("record %d changed when the input buffer was reused: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A truncated container is a truncated container whichever way it is handed in.
func TestReadOCFBytesRejectsATruncatedContainer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	full := buf.Bytes()
	var empty bytes.Buffer
	if err := WriteOCF(&empty, testSchema, CodecDeflate, []ev{}, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	for n := empty.Len() + 1; n < len(full); n++ {
		if _, got, err := ReadOCFBytes(full[:n], unmarshalEv); err == nil {
			t.Fatalf("accepted a container truncated to %d of %d bytes and returned %d records",
				n, len(full), len(got))
		}
	}
	if _, _, err := ReadOCFBytes(nil, unmarshalEv); err == nil {
		t.Error("accepted an empty container")
	}
}

// compress/flate builds roughly a megabyte of compressor state per NewWriter
// and tens of kilobytes per NewReader. A package that writes one small
// container per commit cannot afford either per call, and both types can be
// reset onto a new stream, so they are pooled.
//
// The assertion is on the number of them built, not on bytes allocated: under
// the race detector sync.Pool drops a quarter of what is put back into it on
// purpose, so a byte budget tight enough to mean anything would fail there for
// a reason that has nothing to do with this package. "Fewer compressors than
// containers" holds either way, and is false the moment the pooling goes.
func TestWriteOCFDoesNotBuildACompressorPerCall(t *testing.T) {
	recs := sampleEvents()
	const n = 500
	before := compressorsBuilt.Load()
	for i := 0; i < n; i++ {
		if err := WriteOCF(io.Discard, testSchema, CodecDeflate, recs, marshalEv); err != nil {
			t.Fatal(err)
		}
	}
	built := compressorsBuilt.Load() - before
	t.Logf("%d containers written, %d compressors built", n, built)
	if built > n/2 {
		t.Errorf("writing %d containers built %d compressors; they are meant to be reused", n, built)
	}
}

func TestReadOCFDoesNotBuildADecompressorPerCall(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	body := buf.Bytes()
	const n = 500
	before := decompressorsBuilt.Load()
	for i := 0; i < n; i++ {
		if _, _, err := ReadOCFBytes(body, unmarshalEv); err != nil {
			t.Fatal(err)
		}
	}
	built := decompressorsBuilt.Load() - before
	t.Logf("%d containers read, %d decompressors built", n, built)
	if built > n/2 {
		t.Errorf("reading %d containers built %d decompressors; they are meant to be reused", n, built)
	}
}
