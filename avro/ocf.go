// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// OCF magic: "Obj" + version 1.
var ocfMagic = []byte{'O', 'b', 'j', 1}

// Codec names carried in the OCF header metadata (avro.codec).
const (
	CodecNull    = "null"
	CodecDeflate = "deflate" // raw DEFLATE (compress/flate), the Avro-standard codec
)

// deflateLevel is the compression level CodecDeflate writes at.
//
// It is BestSpeed, not DefaultCompression. A container here is a few kilobytes
// of records on their way to an object store, and at that size level 6 spends
// roughly four times the CPU of level 1 to save a few hundred bytes — a trade
// that reads as free until it is on the latency path of every commit. The
// codec name in the header is unchanged and so is the wire format: level is a
// property of the encoder, not of the stream, and a file written at any level
// is read by any DEFLATE reader.
const deflateLevel = flate.BestSpeed

// A compressor carries roughly a megabyte of window and hash state and a
// decompressor some tens of kilobytes, and both are built per call by
// flate.NewWriter/NewReader. Writing one small container per commit made that
// allocation, not the compression, the dominant cost of a write: it was 99% of
// the bytes a 100-commit run allocated. Both types can be reset onto a new
// stream, so they are pooled and reset instead.
var (
	// compressorsBuilt and decompressorsBuilt count the state this package
	// could not reuse. They exist because "it is pooled" is not a property a
	// test can read off the code: a Pool that is never hit still compiles,
	// still passes every correctness test, and costs exactly what not pooling
	// cost. Counting the constructions is the only honest way to assert that
	// the reuse is real.
	compressorsBuilt   atomic.Int64
	decompressorsBuilt atomic.Int64

	flateWriters = sync.Pool{New: func() any {
		compressorsBuilt.Add(1)
		// The only error NewWriter reports is an out-of-range level, and the
		// level is a constant this package controls.
		w, _ := flate.NewWriter(nil, deflateLevel)
		return w
	}}
	flateReaders = sync.Pool{New: func() any {
		decompressorsBuilt.Add(1)
		return flate.NewReader(nil)
	}}

	// Scratch buffers for the four encodings a WriteOCF builds — header,
	// records, compressed block, block framing. Each is thrown away at the end
	// of the call, and each one grew from nothing on every call: a container
	// written per commit spent more of itself growing buffers than encoding
	// records.
	scratch = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

// borrowEncoder returns an Encoder over a pooled buffer. The caller must
// release it, and must not use it — or anything aliasing its Bytes — after.
func borrowEncoder() *Encoder {
	b := scratch.Get().(*bytes.Buffer)
	b.Reset()
	return &Encoder{buf: b}
}

func releaseEncoder(e *Encoder) { scratch.Put(e.buf) }

// deflateTo compresses src into dst using a pooled compressor.
func deflateTo(dst *bytes.Buffer, src []byte) {
	fw := flateWriters.Get().(*flate.Writer)
	defer flateWriters.Put(fw)
	fw.Reset(dst)
	// flate.Writer's sink is a bytes.Buffer, whose Write is documented never
	// to return an error, so neither of these can fail. See the note on
	// WriteOCF's codec step.
	_, _ = fw.Write(src)
	_ = fw.Close()
}

// inflate decompresses one DEFLATE block using a pooled decompressor. The
// decompressor is returned to the pool either way: Reset reinitialises it, so
// one that stopped on corrupt input is as good as a new one.
func inflate(src []byte) ([]byte, error) {
	fr := flateReaders.Get().(io.ReadCloser)
	defer flateReaders.Put(fr)
	// flate's decompressor reports no error from Reset — there is no dictionary
	// here for it to reject — so this is ignored rather than checked, for the
	// same reason as the compressor's Write and Close: a branch no input can
	// reach reads as a handled case and is not one.
	_ = fr.(flate.Resetter).Reset(bytes.NewReader(src), nil)
	// Start at a plausible decompressed size rather than io.ReadAll's 512
	// bytes: a block that inflates 4:1 otherwise costs four allocations and
	// four copies of itself. The guess is capped so that it stays a guess —
	// the buffer still grows to whatever the block really holds, and the
	// bytes that make it grow are bytes that were actually read.
	buf := bytes.NewBuffer(make([]byte, 0, min(4*len(src), 1<<20)))
	_, err := buf.ReadFrom(fr)
	return buf.Bytes(), err
}

// Marshal writes one record's fields to e, in schema order.
type Marshal[T any] func(e *Encoder, v T)

// Unmarshal reads one record's fields from d, in schema order.
type Unmarshal[T any] func(d *Decoder) (T, error)

// WriteOCF encodes records as a single-block Avro Object Container File: the
// writer's schema (JSON) is embedded in the header — the file is self-describing
// — followed by the (optionally DEFLATE-compressed) record block and a sync
// marker. schemaJSON must be the canonical Avro schema for T. A fresh random sync
// marker is generated per file.
func WriteOCF[T any](w io.Writer, schemaJSON, codec string, records []T, marshal Marshal[T]) error {
	if codec != CodecNull && codec != CodecDeflate {
		return fmt.Errorf("avro: unsupported codec %q", codec)
	}
	var sync [16]byte
	if _, err := randRead(sync[:]); err != nil {
		return err
	}

	// Header: magic, meta map<bytes>{avro.schema, avro.codec}, sync marker.
	h := borrowEncoder()
	defer releaseEncoder(h)
	h.buf.Write(ocfMagic)
	h.Long(2) // two metadata entries
	h.String("avro.schema")
	h.Blob([]byte(schemaJSON))
	h.String("avro.codec")
	h.Blob([]byte(codec))
	h.Long(0) // end of map
	h.buf.Write(sync[:])
	if _, err := w.Write(h.Bytes()); err != nil {
		return err
	}
	if len(records) == 0 {
		return nil // a header-only file is valid (empty container)
	}

	// Serialize the records, then apply the codec.
	body := borrowEncoder()
	defer releaseEncoder(body)
	for _, rec := range records {
		marshal(body, rec)
	}
	payload := body.Bytes()
	if codec == CodecDeflate {
		cb := scratch.Get().(*bytes.Buffer)
		defer scratch.Put(cb)
		cb.Reset()
		// The compressor's sink is a bytes.Buffer, whose Write is documented
		// never to return an error, so neither the Write nor the Close inside
		// deflateTo can fail. They are not checked because the checks would be
		// two branches no test could ever enter, which read as handled cases
		// and are not. If this ever compresses straight to w instead of to
		// memory, both errors become real and must be returned.
		deflateTo(cb, payload)
		payload = cb.Bytes()
	}

	// Data block: object-count, byte-length, payload, sync.
	blk := borrowEncoder()
	defer releaseEncoder(blk)
	blk.Long(int64(len(records)))
	blk.Long(int64(len(payload)))
	if _, err := w.Write(blk.Bytes()); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	_, err := w.Write(sync[:])
	return err
}

// OCFHeader is the parsed self-describing header.
type OCFHeader struct {
	Schema string
	Codec  string
	sync   [16]byte
}

// ReadOCF parses an OCF from r and returns the header schema/codec plus all
// decoded records. It walks every data block (any block count), so it reads
// files written by us or by any spec-conformant Avro writer using the same
// schema and the null/deflate codecs.
func ReadOCF[T any](r io.Reader, unmarshal Unmarshal[T]) (OCFHeader, []T, error) {
	return readOCF(NewDecoder(r), unmarshal)
}

// ReadOCFBytes is ReadOCF for a container that is already in memory — which is
// what an object store's GET hands you, and what this package's own writer
// produces.
//
// It decodes out of b in place instead of copying the file through a window, so
// reading a few-kilobyte container costs neither the copy nor the window. The
// records it returns never alias b: every string and every byte slice a Decoder
// hands out is a copy, so b may be reused or returned to a pool the moment this
// returns.
func ReadOCFBytes[T any](b []byte, unmarshal Unmarshal[T]) (OCFHeader, []T, error) {
	return readOCF(newWindowDecoder(b), unmarshal)
}

func readOCF[T any](d *Decoder, unmarshal Unmarshal[T]) (OCFHeader, []T, error) {
	var hdr OCFHeader

	magic, err := d.raw(4)
	if err != nil {
		return hdr, nil, err
	}
	if !bytes.Equal(magic, ocfMagic) {
		return hdr, nil, errors.New("avro: bad OCF magic")
	}
	meta, err := readMetaMap(d)
	if err != nil {
		return hdr, nil, err
	}
	hdr.Schema = string(meta["avro.schema"])
	hdr.Codec = string(meta["avro.codec"])
	if hdr.Codec == "" {
		hdr.Codec = CodecNull
	}
	if hdr.Codec != CodecNull && hdr.Codec != CodecDeflate {
		return hdr, nil, fmt.Errorf("avro: unsupported codec %q", hdr.Codec)
	}
	sync, err := d.raw(16)
	if err != nil {
		return hdr, nil, err
	}
	copy(hdr.sync[:], sync)

	var out []T
	sized := false
	for {
		count, err := d.Long()
		if err == io.EOF {
			return hdr, out, nil // clean end of container
		}
		if err != nil {
			return hdr, out, err
		}
		size, err := d.Long()
		if err != nil {
			return hdr, out, err
		}
		// BUG-AVRO-1: both numbers come straight from the file. A negative size
		// used to reach make([]byte, size) and panic the process (makeslice: len
		// out of range); a negative count silently decoded nothing. And a huge
		// size was allocated up front before a single byte was read, so a few
		// bytes of corrupt header could demand gigabytes. Reject the negatives and
		// let the decoder's window grow into the block, so memory grows only with
		// bytes that exist.
		if count < 0 {
			return hdr, out, fmt.Errorf("avro: negative block record count %d (corrupt block)", count)
		}
		if size < 0 {
			return hdr, out, fmt.Errorf("avro: negative block size %d (corrupt block)", size)
		}
		// The block is decoded out of the window the framing was read from: one
		// copy of the file's bytes, and the records are cut from it in place.
		payload, err := d.raw(size)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return hdr, out, fmt.Errorf("avro: block truncated: %d of %d bytes: %w",
					len(d.buf)-d.pos, size, io.ErrUnexpectedEOF)
			}
			return hdr, out, err
		}
		if hdr.Codec == CodecDeflate {
			dec, err := inflate(payload)
			if err != nil {
				return hdr, out, fmt.Errorf("avro: inflate block: %w", err)
			}
			payload = dec
		}
		if !sized {
			// One allocation for the records instead of a dozen doublings.
			// The hint is the block's record count CLAMPED TO THE BYTES THAT
			// WERE ACTUALLY READ: count comes straight from the file, so on
			// its own it is BUG-AVRO-1 again — a few bytes of corrupt header
			// demanding gigabytes. No record encodes in fewer than one byte,
			// so the decoded block's length is an honest ceiling on how many
			// of them it can hold.
			out = make([]T, 0, min(count, int64(len(payload))))
			sized = true
		}
		bd := newWindowDecoder(payload)
		for i := int64(0); i < count; i++ {
			rec, err := unmarshal(bd)
			if err != nil {
				return hdr, out, fmt.Errorf("avro: decode record %d: %w", i, err)
			}
			out = append(out, rec)
		}
		blockSync, err := d.raw(16)
		if err != nil {
			return hdr, out, err
		}
		if !bytes.Equal(blockSync, hdr.sync[:]) {
			return hdr, out, errors.New("avro: sync marker mismatch (corrupt block)")
		}
	}
}

// readMetaMap decodes the OCF header's map<bytes>, handling multi-block maps.
func readMetaMap(d *Decoder) (map[string][]byte, error) {
	out := map[string][]byte{}
	for {
		count, err := d.Long()
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return out, nil
		}
		if count < 0 { // block-with-size form
			count = -count
			if _, err := d.Long(); err != nil {
				return nil, err
			}
		}
		for i := int64(0); i < count; i++ {
			k, err := d.String()
			if err != nil {
				return nil, err
			}
			v, err := d.Blob()
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
	}
}

// randRead supplies the per-file sync marker. It is a variable so a test can
// make it fail: a container written with a sync marker that was never
// generated would be silently unreadable, so the error has to be returned, and
// an error that is returned but never exercised is not known to work.
var randRead = rand.Read
