// SPDX-License-Identifier: BSD-3-Clause

// Package avro is a dependency-free (standard-library-only) implementation of
// the Apache Avro binary encoding and the Avro Object Container File (OCF)
// format — enough of the spec to persist typed records to object storage in a
// self-describing, splittable, compressed container.
//
// # Avro is not ours
//
// Apache Avro is a data serialisation format created and maintained by the
// Apache Software Foundation, specified at https://avro.apache.org. This
// package is an independent implementation of that public specification. It is
// not affiliated with, endorsed by, or a product of the Apache Software
// Foundation, and "Apache Avro" is their trademark, not ours. What is ours is
// this Go code and its bugs.
//
// # Why another one
//
// The established Go implementations are good and carry dependencies. This one
// exists because the rest of this module promises zero dependencies, and a
// container format is not worth breaking that promise for. If you are already
// using github.com/hamba/avro or github.com/linkedin/goavro, they cover more of
// the specification than this does and you should keep using them.
//
// # What is implemented, and what is not
//
// Implemented: the binary encoding for the primitive types, records, arrays,
// maps, unions and enums; OCF read and write with the null and deflate codecs;
// the sync marker and block framing that make a file splittable.
//
// NOT implemented: schema resolution between a writer's and a reader's schema,
// schema registries, RPC, logical types beyond what is noted in the type table,
// and the snappy and zstd codecs. If you need writer/reader schema evolution,
// this package is the wrong tool — it reads a file with the schema that file
// carries.
package avro

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ── binary primitives ────────────────────────────────────────────────────────

// Encoder writes Avro binary primitives to a buffer.
type Encoder struct{ buf *bytes.Buffer }

// NewEncoder returns an Encoder backed by a fresh buffer.
func NewEncoder() *Encoder { return &Encoder{buf: new(bytes.Buffer)} }

// Bytes returns the accumulated encoding.
func (e *Encoder) Bytes() []byte { return e.buf.Bytes() }

// Reset clears the buffer for reuse.
func (e *Encoder) Reset() { e.buf.Reset() }

// Long writes a variable-length zig-zag encoded 64-bit integer (Avro long/int).
func (e *Encoder) Long(v int64) {
	// zig-zag: map signed to unsigned so small magnitudes stay short.
	u := uint64((v << 1) ^ (v >> 63))
	for u&^0x7f != 0 {
		e.buf.WriteByte(byte(u&0x7f) | 0x80)
		u >>= 7
	}
	e.buf.WriteByte(byte(u))
}

// Int is Avro int — same wire form as long.
func (e *Encoder) Int(v int32) { e.Long(int64(v)) }

// Bool writes a single 0/1 byte.
func (e *Encoder) Bool(v bool) {
	if v {
		e.buf.WriteByte(1)
	} else {
		e.buf.WriteByte(0)
	}
}

// Bytes writes a length-prefixed byte slice.
func (e *Encoder) Blob(b []byte) {
	e.Long(int64(len(b)))
	e.buf.Write(b)
}

// String writes a length-prefixed UTF-8 string.
func (e *Encoder) String(s string) {
	e.Long(int64(len(s)))
	e.buf.WriteString(s)
}

// Double writes an 8-byte little-endian IEEE-754 double.
func (e *Encoder) Double(v float64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	e.buf.Write(b[:])
}

// Float writes a 4-byte little-endian IEEE-754 float.
func (e *Encoder) Float(v float32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
	e.buf.Write(b[:])
}

// OptionalString encodes a union {null, string}: index 0 = null, 1 = value.
func (e *Encoder) OptionalString(s string, present bool) {
	if !present {
		e.Long(0)
		return
	}
	e.Long(1)
	e.String(s)
}

// Float64Array encodes an array<double> as a single positive block + terminator.
func (e *Encoder) Float64Array(xs []float64) {
	if len(xs) > 0 {
		e.Long(int64(len(xs)))
		for _, x := range xs {
			e.Double(x)
		}
	}
	e.Long(0) // end-of-array marker
}

// Float64Matrix encodes an array<array<double>> — the shape of a set of
// embedding vectors — as an outer block of inner arrays + terminator.
func (e *Encoder) Float64Matrix(m [][]float64) {
	if len(m) > 0 {
		e.Long(int64(len(m)))
		for _, row := range m {
			e.Float64Array(row)
		}
	}
	e.Long(0)
}

// Decoder reads Avro binary primitives from a buffered reader.
type Decoder struct{ r *bufio.Reader }

// NewDecoder wraps r.
func NewDecoder(r io.Reader) *Decoder {
	if br, ok := r.(*bufio.Reader); ok {
		return &Decoder{r: br}
	}
	return &Decoder{r: bufio.NewReader(r)}
}

// Long reads a zig-zag varint.
func (d *Decoder) Long() (int64, error) {
	var u uint64
	var shift uint
	for {
		b, err := d.r.ReadByte()
		if err != nil {
			return 0, err
		}
		u |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 64 {
			return 0, errors.New("avro: varint overflow")
		}
	}
	return int64(u>>1) ^ -int64(u&1), nil
}

// Int reads an Avro int.
func (d *Decoder) Int() (int32, error) {
	v, err := d.Long()
	return int32(v), err
}

// Bool reads a single 0/1 byte.
func (d *Decoder) Bool() (bool, error) {
	b, err := d.r.ReadByte()
	return b != 0, err
}

// Blob reads a length-prefixed byte slice.
func (d *Decoder) Blob() ([]byte, error) {
	n, err := d.Long()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("avro: negative length %d", n)
	}
	// Read through a LimitReader rather than make([]byte, n): n comes from the
	// input, and a corrupt length must not allocate more than the bytes present.
	b, err := io.ReadAll(io.LimitReader(d.r, n))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != n {
		return nil, io.ErrUnexpectedEOF
	}
	return b, nil
}

// String reads a length-prefixed UTF-8 string.
func (d *Decoder) String() (string, error) {
	b, err := d.Blob()
	return string(b), err
}

// Double reads an 8-byte little-endian double.
func (d *Decoder) Double() (float64, error) {
	var b [8]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b[:])), nil
}

// Float reads a 4-byte little-endian float.
func (d *Decoder) Float() (float32, error) {
	var b [4]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b[:])), nil
}

// OptionalString decodes a union {null, string}. present=false means null.
func (d *Decoder) OptionalString() (s string, present bool, err error) {
	idx, err := d.Long()
	if err != nil {
		return "", false, err
	}
	if idx == 0 {
		return "", false, nil
	}
	s, err = d.String()
	return s, true, err
}

// Float64Array decodes an array<double> written as positive blocks + terminator.
func (d *Decoder) Float64Array() ([]float64, error) {
	var out []float64
	for {
		count, err := d.Long()
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return out, nil
		}
		if count < 0 { // block-with-size form: negate count, then skip the size long
			count = -count
			if _, err := d.Long(); err != nil {
				return nil, err
			}
		}
		for i := int64(0); i < count; i++ {
			x, err := d.Double()
			if err != nil {
				return nil, err
			}
			out = append(out, x)
		}
	}
}

// Float64Matrix decodes an array<array<double>>.
func (d *Decoder) Float64Matrix() ([][]float64, error) {
	var out [][]float64
	for {
		count, err := d.Long()
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return out, nil
		}
		if count < 0 {
			count = -count
			if _, err := d.Long(); err != nil {
				return nil, err
			}
		}
		for i := int64(0); i < count; i++ {
			row, err := d.Float64Array()
			if err != nil {
				return nil, err
			}
			out = append(out, row)
		}
	}
}
