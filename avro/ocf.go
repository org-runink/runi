// SPDX-License-Identifier: BSD-3-Clause

package avro

import (
	"bufio"
	"bytes"
	"compress/flate"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// OCF magic: "Obj" + version 1.
var ocfMagic = []byte{'O', 'b', 'j', 1}

// Codec names carried in the OCF header metadata (avro.codec).
const (
	CodecNull    = "null"
	CodecDeflate = "deflate" // raw DEFLATE (compress/flate), the Avro-standard codec
)

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
	if _, err := rand.Read(sync[:]); err != nil {
		return err
	}

	// Header: magic, meta map<bytes>{avro.schema, avro.codec}, sync marker.
	h := NewEncoder()
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
	body := NewEncoder()
	for _, rec := range records {
		marshal(body, rec)
	}
	payload := body.Bytes()
	if codec == CodecDeflate {
		var cb bytes.Buffer
		fw, _ := flate.NewWriter(&cb, flate.DefaultCompression)
		if _, err := fw.Write(payload); err != nil {
			return err
		}
		if err := fw.Close(); err != nil {
			return err
		}
		payload = cb.Bytes()
	}

	// Data block: object-count, byte-length, payload, sync.
	blk := NewEncoder()
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
	br := bufio.NewReader(r)
	var hdr OCFHeader

	magic := make([]byte, 4)
	if _, err := io.ReadFull(br, magic); err != nil {
		return hdr, nil, err
	}
	if !bytes.Equal(magic, ocfMagic) {
		return hdr, nil, errors.New("avro: bad OCF magic")
	}
	meta, err := readMetaMap(br)
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
	if _, err := io.ReadFull(br, hdr.sync[:]); err != nil {
		return hdr, nil, err
	}

	var out []T
	d := NewDecoder(br)
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
		// read through a LimitReader, so memory grows only with bytes that exist.
		if count < 0 {
			return hdr, out, fmt.Errorf("avro: negative block record count %d (corrupt block)", count)
		}
		if size < 0 {
			return hdr, out, fmt.Errorf("avro: negative block size %d (corrupt block)", size)
		}
		payload, err := io.ReadAll(io.LimitReader(br, size))
		if err != nil {
			return hdr, out, err
		}
		if int64(len(payload)) != size {
			return hdr, out, fmt.Errorf("avro: block truncated: %d of %d bytes: %w",
				len(payload), size, io.ErrUnexpectedEOF)
		}
		if hdr.Codec == CodecDeflate {
			fr := flate.NewReader(bytes.NewReader(payload))
			dec, err := io.ReadAll(fr)
			fr.Close()
			if err != nil {
				return hdr, out, fmt.Errorf("avro: inflate block: %w", err)
			}
			payload = dec
		}
		bd := NewDecoder(bytes.NewReader(payload))
		for i := int64(0); i < count; i++ {
			rec, err := unmarshal(bd)
			if err != nil {
				return hdr, out, fmt.Errorf("avro: decode record %d: %w", i, err)
			}
			out = append(out, rec)
		}
		var sync [16]byte
		if _, err := io.ReadFull(br, sync[:]); err != nil {
			return hdr, out, err
		}
		if sync != hdr.sync {
			return hdr, out, errors.New("avro: sync marker mismatch (corrupt block)")
		}
	}
}

// readMetaMap decodes the OCF header's map<bytes>, handling multi-block maps.
func readMetaMap(br *bufio.Reader) (map[string][]byte, error) {
	d := NewDecoder(br)
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
