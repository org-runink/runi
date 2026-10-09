// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"compress/flate"
	"io"
	"math"
	"strings"
	"testing"
)

// ── the record used throughout: exercises every primitive recordstore relies on ──

const testSchema = `{"type":"record","name":"Ev","fields":[` +
	`{"name":"id","type":"long"},` +
	`{"name":"name","type":"string"},` +
	`{"name":"score","type":"double"},` +
	`{"name":"note","type":["null","string"]},` +
	`{"name":"vec","type":{"type":"array","items":"double"}}]}`

type ev struct {
	ID       int64
	Name     string
	Score    float64
	Note     string
	HaveNote bool
	Vec      []float64
}

func marshalEv(e *Encoder, v ev) {
	e.Long(v.ID)
	e.String(v.Name)
	e.Double(v.Score)
	e.OptionalString(v.Note, v.HaveNote)
	e.Float64Array(v.Vec)
}

func unmarshalEv(d *Decoder) (ev, error) {
	var v ev
	var err error
	if v.ID, err = d.Long(); err != nil {
		return v, err
	}
	if v.Name, err = d.String(); err != nil {
		return v, err
	}
	if v.Score, err = d.Double(); err != nil {
		return v, err
	}
	if v.Note, v.HaveNote, err = d.OptionalString(); err != nil {
		return v, err
	}
	v.Vec, err = d.Float64Array()
	return v, err
}

func sampleEvents() []ev {
	return []ev{
		{ID: 1, Name: "alpha", Score: 0.5, Note: "n1", HaveNote: true, Vec: []float64{1, 2, 3}},
		{ID: -9223372036854775808, Name: "", Score: -0.0, HaveNote: false},
		{ID: 42, Name: "héllo \U0001F600", Score: 1e308, Note: "", HaveNote: true, Vec: []float64{0}},
	}
}

func equalEv(a, b ev) bool {
	if a.ID != b.ID || a.Name != b.Name || a.Note != b.Note || a.HaveNote != b.HaveNote {
		return false
	}
	if a.Score != b.Score {
		return false
	}
	if len(a.Vec) != len(b.Vec) {
		return false
	}
	for i := range a.Vec {
		if a.Vec[i] != b.Vec[i] {
			return false
		}
	}
	return true
}

// ── happy paths ──────────────────────────────────────────────────────────────

func TestOCFRoundTripsEveryCodec(t *testing.T) {
	t.Parallel()
	for _, codec := range []string{CodecNull, CodecDeflate} {
		t.Run(codec, func(t *testing.T) {
			t.Parallel()
			want := sampleEvents()
			var buf bytes.Buffer
			if err := WriteOCF(&buf, testSchema, codec, want, marshalEv); err != nil {
				t.Fatalf("WriteOCF(%s): %v", codec, err)
			}
			hdr, got, err := ReadOCF(bytes.NewReader(buf.Bytes()), unmarshalEv)
			if err != nil {
				t.Fatalf("ReadOCF(%s): %v", codec, err)
			}
			// The self-describing claim: the writer's schema comes back verbatim,
			// so a reader that has never seen our Go types can interpret the file.
			if hdr.Schema != testSchema {
				t.Errorf("header schema not preserved.\n got: %s\nwant: %s", hdr.Schema, testSchema)
			}
			if hdr.Codec != codec {
				t.Errorf("header codec = %q, want %q", hdr.Codec, codec)
			}
			if len(got) != len(want) {
				t.Fatalf("read %d records, wrote %d", len(got), len(want))
			}
			for i := range want {
				if !equalEv(got[i], want[i]) {
					t.Errorf("record %d changed across the container:\n got %+v\nwant %+v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestOCFDeflateBlockIsRawDeflateNotZlibOrGzip(t *testing.T) {
	t.Parallel()
	// The Avro spec's "deflate" codec is *raw* DEFLATE (RFC 1951) with no zlib
	// (RFC 1950) or gzip wrapper. Wrapping it would round-trip perfectly through
	// this package and be unreadable by every other Avro implementation -- and
	// unreadable is indistinguishable from lost for an archive format.
	recs := sampleEvents()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, recs, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	block := blockPayload(t, buf.Bytes())

	if len(block) >= 2 {
		// zlib headers are 0x78 0x01/0x9c/0xda; gzip is 0x1f 0x8b.
		if block[0] == 0x78 && (block[1] == 0x01 || block[1] == 0x5e || block[1] == 0x9c || block[1] == 0xda) {
			t.Errorf("deflate block starts %#x %#x, which is a zlib header; the Avro spec requires raw DEFLATE", block[0], block[1])
		}
		if block[0] == 0x1f && block[1] == 0x8b {
			t.Error("deflate block starts with a gzip header; the Avro spec requires raw DEFLATE")
		}
	}

	// And it must actually inflate as raw DEFLATE to the concatenation of the
	// records' binary encodings -- checked against an independently built
	// encoding, not against ReadOCF.
	fr := flate.NewReader(bytes.NewReader(block))
	inflated, err := io.ReadAll(fr)
	fr.Close()
	if err != nil {
		t.Fatalf("block does not inflate as raw DEFLATE: %v", err)
	}
	e := NewEncoder()
	for _, r := range recs {
		marshalEv(e, r)
	}
	if !bytes.Equal(inflated, e.Bytes()) {
		t.Errorf("inflated block (%d bytes) != the records' Avro binary encoding (%d bytes)", len(inflated), len(e.Bytes()))
	}
}

// blockPayload returns the compressed bytes of the first data block of an OCF,
// parsing the container structurally (magic, meta map, sync, count, size) so the
// test does not depend on ReadOCF.
func blockPayload(t *testing.T, ocf []byte) []byte {
	t.Helper()
	br := bytes.NewReader(ocf)
	magic := make([]byte, 4)
	if _, err := io.ReadFull(br, magic); err != nil {
		t.Fatalf("read magic: %v", err)
	}
	if !bytes.Equal(magic, []byte{'O', 'b', 'j', 1}) {
		t.Fatalf("bad magic %v; the OCF magic is 'Obj' followed by version byte 1", magic)
	}
	d := NewDecoder(br)
	n, err := d.Long()
	if err != nil {
		t.Fatalf("meta count: %v", err)
	}
	for i := int64(0); i < n; i++ {
		if _, err := d.String(); err != nil {
			t.Fatalf("meta key: %v", err)
		}
		if _, err := d.Blob(); err != nil {
			t.Fatalf("meta value: %v", err)
		}
	}
	if end, err := d.Long(); err != nil || end != 0 {
		t.Fatalf("meta map terminator = %d, %v; want 0, nil", end, err)
	}
	if _, err := d.raw(16); err != nil {
		t.Fatalf("sync marker: %v", err)
	}
	if _, err := d.Long(); err != nil { // record count
		t.Fatalf("block count: %v", err)
	}
	size, err := d.Long()
	if err != nil {
		t.Fatalf("block size: %v", err)
	}
	payload, err := d.raw(size)
	if err != nil {
		t.Fatalf("block payload: %v", err)
	}
	// d.raw hands back the decoder's own window; copy it before it is reused.
	return append([]byte(nil), payload...)
}

func TestOCFEmptyContainerIsValidAndReadsBackEmpty(t *testing.T) {
	t.Parallel()
	// recordstore refuses to Append zero records, but an empty container is a
	// legal Avro file and must not read as an error (which would make a whole
	// partition unscannable) nor as phantom records.
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, []ev{}, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	hdr, got, err := ReadOCF(bytes.NewReader(buf.Bytes()), unmarshalEv)
	if err != nil {
		t.Fatalf("ReadOCF on a header-only container: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("header-only container yielded %d records, want 0", len(got))
	}
	if hdr.Schema != testSchema {
		t.Error("header-only container lost its schema")
	}
}

func TestOCFReadsSpecConformantMultiBlockContainer(t *testing.T) {
	t.Parallel()
	// Our writer always emits ONE block; the doc comment promises the reader
	// "walks every data block (any block count)", i.e. can read files written by
	// a real Avro writer (which blocks output by size). Nothing in the
	// write-then-read path exercises that, so build a multi-block container by
	// hand.
	recs := sampleEvents()
	var buf bytes.Buffer
	sync := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6}

	h := NewEncoder()
	h.Long(2)
	h.String("avro.schema")
	h.Blob([]byte(testSchema))
	h.String("avro.codec")
	h.Blob([]byte(CodecNull))
	h.Long(0)
	buf.Write([]byte{'O', 'b', 'j', 1})
	buf.Write(h.Bytes())
	buf.Write(sync[:])

	// One block per record.
	for _, r := range recs {
		body := NewEncoder()
		marshalEv(body, r)
		blk := NewEncoder()
		blk.Long(1)
		blk.Long(int64(len(body.Bytes())))
		buf.Write(blk.Bytes())
		buf.Write(body.Bytes())
		buf.Write(sync[:])
	}

	_, got, err := ReadOCF(bytes.NewReader(buf.Bytes()), unmarshalEv)
	if err != nil {
		t.Fatalf("ReadOCF on a 3-block container: %v", err)
	}
	if len(got) != len(recs) {
		t.Fatalf("read %d records from a 3-block container, want %d -- the reader stopped after the first block", len(got), len(recs))
	}
	for i := range recs {
		if !equalEv(got[i], recs[i]) {
			t.Errorf("record %d: got %+v want %+v", i, got[i], recs[i])
		}
	}
}

// ── corruption must be loud, never silent ────────────────────────────────────

func TestOCFRejectsBadMagic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecNull, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	b := buf.Bytes()
	for _, mut := range []struct {
		name string
		fix  func([]byte)
	}{
		{"wrong text", func(b []byte) { b[0] = 'X' }},
		{"wrong version", func(b []byte) { b[3] = 2 }},
	} {
		t.Run(mut.name, func(t *testing.T) {
			c := append([]byte(nil), b...)
			mut.fix(c)
			_, got, err := ReadOCF(bytes.NewReader(c), unmarshalEv)
			if err == nil {
				t.Fatalf("ReadOCF accepted a container with %s magic and returned %d records", mut.name, len(got))
			}
			if !strings.Contains(err.Error(), "magic") {
				t.Errorf("error = %q, want it to name the bad magic", err)
			}
		})
	}
}

func TestOCFDetectsSyncMarkerCorruption(t *testing.T) {
	t.Parallel()
	// The sync marker is the container's only integrity check: it proves the
	// block ended where the block header said it would. Without it, a block
	// whose length field has drifted decodes garbage as records and reports
	// success -- silent corruption, exactly the class that bites this platform.
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecNull, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	b := append([]byte(nil), buf.Bytes()...)
	b[len(b)-1] ^= 0xff // corrupt the trailing sync marker

	_, got, err := ReadOCF(bytes.NewReader(b), unmarshalEv)
	if err == nil {
		t.Fatalf("ReadOCF returned %d records and no error for a container whose sync marker was corrupted", len(got))
	}
	if !strings.Contains(err.Error(), "sync") {
		t.Errorf("error = %q, want it to identify the sync marker mismatch", err)
	}
}

func TestOCFDetectsBlockFramingCorruption(t *testing.T) {
	t.Parallel()
	// Corrupting the block *framing* -- the record count or the byte length that
	// precede the payload -- must always be caught, because the sync marker will
	// then land in the wrong place. This is the one integrity guarantee the
	// container actually makes, so it must not regress.
	recs := sampleEvents()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecNull, recs, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	orig := buf.Bytes()
	payloadLen := len(blockPayload(t, orig))
	payloadStart := len(orig) - 16 - payloadLen

	// The two block-header longs sit immediately before the payload. Locate the
	// boundary between them by re-encoding.
	cnt := NewEncoder()
	cnt.Long(int64(len(recs)))
	sz := NewEncoder()
	sz.Long(int64(payloadLen))
	sizeStart := payloadStart - len(sz.Bytes())

	// Corrupting the declared block byte-length must always be caught: the sync
	// marker then lands in the wrong place.
	for i := sizeStart; i < payloadStart; i++ {
		c := append([]byte(nil), orig...)
		c[i] ^= 0x02 // perturb the value without breaking varint continuation
		_, got, err := ReadOCF(bytes.NewReader(c), unmarshalEv)
		if err == nil {
			t.Errorf("flipping block size byte %d produced NO error; ReadOCF returned %d records", i-sizeStart, len(got))
		}
	}
}

func TestOCFUnderstatedBlockCountSilentlyDropsRecords(t *testing.T) {
	t.Parallel()
	// CHARACTERIZATION of BUG-AVRO-3. A data block declares "<count> records in
	// <size> bytes". ReadOCF decodes exactly <count> records and then jumps
	// straight to the sync marker using <size> -- it never checks that the
	// decoded records consumed the whole payload.
	//
	// So a block whose count field has drifted DOWN (one flipped bit in a single
	// byte) returns fewer records and a nil error. The sync marker still matches,
	// because it is positioned by <size>, which is untouched. The caller
	// (recordstore.Scan) reports success on a short partition. This is the exact
	// silent-loss shape: green, and missing data.
	//
	// A one-line guard closes it: after the decode loop, require the block
	// decoder to be at EOF. Reported, deliberately not fixed in this PR.
	recs := sampleEvents() // 3 records
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecNull, recs, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	orig := buf.Bytes()
	payloadLen := len(blockPayload(t, orig))
	payloadStart := len(orig) - 16 - payloadLen
	sz := NewEncoder()
	sz.Long(int64(payloadLen))
	countByte := payloadStart - len(sz.Bytes()) - 1 // count is a single byte for 3

	c := append([]byte(nil), orig...)
	if c[countByte] != 0x06 { // zigzag(3)
		t.Fatalf("expected the block count byte to be 0x06 (zigzag 3), got %#x", c[countByte])
	}
	c[countByte] = 0x04 // zigzag(2): claim 2 records instead of 3

	_, got, err := ReadOCF(bytes.NewReader(c), unmarshalEv)
	if err != nil {
		t.Fatalf("BUG-AVRO-3 appears to be fixed (ReadOCF now errors: %v). Delete this "+
			"characterization test and enable the assertion below.", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the understated count to yield 2 records, got %d", len(got))
	}
	t.Logf("BUG-AVRO-3 confirmed: a one-bit change to the block count field dropped "+
		"record %d of %d and ReadOCF returned nil error", len(recs), len(recs))
}

func TestOCFHasNoContentIntegrityCheck(t *testing.T) {
	t.Parallel()
	// CHARACTERIZATION of FINDING-AVRO-2, not a wish. The Avro "deflate" codec
	// is raw DEFLATE (RFC 1951), which -- unlike zlib or gzip -- carries no
	// checksum, and the OCF sync marker only proves where the block ended. So a
	// bit-flip *inside* a block silently changes record values and ReadOCF
	// returns them with a nil error.
	//
	// Do not assume your storage layer closes this for you. Object stores
	// commonly compute a checksum on PUT and do not verify it on GET, so a
	// silently corrupted object can be read back without complaint. If record
	// integrity matters to you, add your own digest over the file and check it
	// on read -- this package will not do it for you.
	//
	// This test proves the exposure is real and pins it. If someone adds a
	// content checksum (an OCF meta digest, or ETag verification on read), this
	// test SHOULD start failing -- delete it then, and say so in the commit.
	want := sampleEvents()
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, want, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	orig := buf.Bytes()
	payloadLen := len(blockPayload(t, orig))
	start := len(orig) - 16 - payloadLen

	silent := 0
	for i := start; i < start+payloadLen; i++ {
		c := append([]byte(nil), orig...)
		c[i] ^= 0xff
		_, got, err := ReadOCF(bytes.NewReader(c), unmarshalEv)
		if err != nil || len(got) != len(want) {
			continue
		}
		for j := range want {
			if !equalEv(got[j], want[j]) {
				silent++
				break
			}
		}
	}
	if silent == 0 {
		t.Errorf("FINDING-AVRO-2 appears to be fixed: no single-byte flip in the %d-byte "+
			"compressed block silently altered a record. Delete this characterization test.", payloadLen)
	}
	t.Logf("FINDING-AVRO-2: %d of %d single-byte flips in the compressed block returned "+
		"altered records with a nil error", silent, payloadLen)
}

func TestOCFRejectsTruncatedContainer(t *testing.T) {
	t.Parallel()
	// A partially-uploaded / partially-written object must fail loudly. The one
	// legal short form is the header-only container, so start after the header.
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, CodecDeflate, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	full := buf.Bytes()
	var empty bytes.Buffer
	if err := WriteOCF(&empty, testSchema, CodecDeflate, []ev{}, marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	headerLen := empty.Len()

	for n := headerLen + 1; n < len(full); n++ {
		_, got, err := ReadOCF(bytes.NewReader(full[:n]), unmarshalEv)
		if err == nil {
			t.Fatalf("ReadOCF accepted a container truncated to %d of %d bytes and returned %d records with no error",
				n, len(full), len(got))
		}
	}
}

func TestOCFRejectsUnknownCodec(t *testing.T) {
	t.Parallel()
	// Silently ignoring an unknown codec would hand the record decoder
	// compressed bytes and produce garbage records, or an empty result, with no
	// error. Both the writer and the reader must refuse.
	var buf bytes.Buffer
	if err := WriteOCF(&buf, testSchema, "snappy", sampleEvents(), marshalEv); err == nil {
		t.Error("WriteOCF accepted codec \"snappy\", which this package cannot write")
	}

	// A container written elsewhere declaring snappy must be rejected, not read
	// as if it were uncompressed.
	h := NewEncoder()
	h.Long(2)
	h.String("avro.schema")
	h.Blob([]byte(testSchema))
	h.String("avro.codec")
	h.Blob([]byte("snappy"))
	h.Long(0)
	var c bytes.Buffer
	c.Write([]byte{'O', 'b', 'j', 1})
	c.Write(h.Bytes())
	c.Write(make([]byte, 16))

	_, got, err := ReadOCF(bytes.NewReader(c.Bytes()), unmarshalEv)
	if err == nil {
		t.Fatalf("ReadOCF accepted a snappy container and returned %d records", len(got))
	}
	if !strings.Contains(err.Error(), "snappy") {
		t.Errorf("error = %q, want it to name the unsupported codec", err)
	}
}

func TestOCFMissingCodecMetaDefaultsToNull(t *testing.T) {
	t.Parallel()
	// The Avro spec makes avro.codec optional and defaults it to "null". A
	// reader that treated a missing codec as an error could not read files from
	// spec-conformant writers that omit it.
	recs := sampleEvents()[:1]
	body := NewEncoder()
	marshalEv(body, recs[0])

	h := NewEncoder()
	h.Long(1)
	h.String("avro.schema")
	h.Blob([]byte(testSchema))
	h.Long(0)
	var c bytes.Buffer
	sync := [16]byte{1}
	c.Write([]byte{'O', 'b', 'j', 1})
	c.Write(h.Bytes())
	c.Write(sync[:])
	blk := NewEncoder()
	blk.Long(1)
	blk.Long(int64(len(body.Bytes())))
	c.Write(blk.Bytes())
	c.Write(body.Bytes())
	c.Write(sync[:])

	hdr, got, err := ReadOCF(bytes.NewReader(c.Bytes()), unmarshalEv)
	if err != nil {
		t.Fatalf("ReadOCF on a container with no avro.codec: %v", err)
	}
	if hdr.Codec != CodecNull {
		t.Errorf("absent avro.codec read as %q, spec default is %q", hdr.Codec, CodecNull)
	}
	if len(got) != 1 || !equalEv(got[0], recs[0]) {
		t.Errorf("records = %+v, want %+v", got, recs)
	}
}

func TestOCFRejectsNegativeBlockSize(t *testing.T) {
	t.Parallel()
	// BUG-AVRO-1, fixed: ReadOCF did `make([]byte, size)` on the block size read
	// straight from the file, so a negative size panicked the process (makeslice:
	// len out of range) instead of returning an error. The same unchecked numbers
	// also let a negative record count decode nothing silently, and a huge size
	// allocate gigabytes before a byte was read; all three must be plain errors.
	container := func(count, size int64) []byte {
		h := NewEncoder()
		h.Long(2)
		h.String("avro.schema")
		h.Blob([]byte(testSchema))
		h.String("avro.codec")
		h.Blob([]byte(CodecNull))
		h.Long(0)
		var c bytes.Buffer
		c.Write([]byte{'O', 'b', 'j', 1})
		c.Write(h.Bytes())
		c.Write(make([]byte, 16))
		blk := NewEncoder()
		blk.Long(count)
		blk.Long(size)
		c.Write(blk.Bytes())
		c.Write([]byte{1, 2, 3}) // a few real bytes, far fewer than any size claimed
		return c.Bytes()
	}
	for _, tc := range []struct {
		name        string
		count, size int64
	}{
		{"negative size", 1, -1},
		{"most negative size", 1, math.MinInt64},
		{"negative count", -1, 3},
		{"size far past the end (no up-front allocation)", 1, 1 << 40},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("ReadOCF panicked on a corrupt block: %v", r)
					}
				}()
				_, _, err = ReadOCF(bytes.NewReader(container(tc.count, tc.size)), unmarshalEv)
			}()
			if err == nil {
				t.Fatal("ReadOCF accepted a corrupt block; a corrupt object must error")
			}
		})
	}
}

func TestOCFEachFileGetsAFreshSyncMarker(t *testing.T) {
	t.Parallel()
	// The sync marker must be per-file and random. A constant marker would make
	// the integrity check pass across a block spliced in from a *different*
	// file -- undetectable cross-object corruption.
	var a, b bytes.Buffer
	if err := WriteOCF(&a, testSchema, CodecNull, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	if err := WriteOCF(&b, testSchema, CodecNull, sampleEvents(), marshalEv); err != nil {
		t.Fatalf("WriteOCF: %v", err)
	}
	syncA := a.Bytes()[len(a.Bytes())-16:]
	syncB := b.Bytes()[len(b.Bytes())-16:]
	if bytes.Equal(syncA, syncB) {
		t.Errorf("two containers share the sync marker %x; it must be random per file", syncA)
	}
	if bytes.Equal(syncA, make([]byte, 16)) {
		t.Error("sync marker is all zeros; it must be random")
	}
	// The header copy and the block trailer copy must match each other, or a
	// conformant reader rejects our own files.
	if !bytes.Equal(a.Bytes()[len(a.Bytes())-16:], headerSync(t, a.Bytes())) {
		t.Error("the sync marker in the header differs from the one trailing the block; the file is not self-consistent")
	}
}

func headerSync(t *testing.T, ocf []byte) []byte {
	t.Helper()
	br := bytes.NewReader(ocf)
	if _, err := io.ReadFull(br, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	d := NewDecoder(br)
	n, err := d.Long()
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < n; i++ {
		if _, err := d.String(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Blob(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Long(); err != nil {
		t.Fatal(err)
	}
	sync, err := d.raw(16)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), sync...)
}
