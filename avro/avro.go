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
	// Built in a stack array and written once. Writing it a byte at a time
	// paid bytes.Buffer's grow check per byte, and every record here is mostly
	// varints.
	var b [maxVarintLen]byte
	n := 0
	for u&^0x7f != 0 {
		b[n] = byte(u&0x7f) | 0x80
		u >>= 7
		n++
	}
	b[n] = byte(u)
	e.buf.Write(b[:n+1])
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

// Decoder reads Avro binary primitives out of a window of bytes, refilling the
// window from an io.Reader when it runs dry.
//
// The window is the point. Decoding a value is a bounds check and an index into
// a []byte, not a ReadByte or a ReadFull through an io.Reader: an indirect call
// per value, plus — for every fixed-width type — a local array that escapes to
// the heap because it is passed as an interface argument. That overhead, not
// the arithmetic, is what made reading a container cost ten times what writing
// one did. A Decoder over bytes already in memory does no I/O at all.
type Decoder struct {
	buf []byte    // the window; buf[pos:] is unread
	pos int       // read offset into buf
	src io.Reader // refills the window; nil once there are no more bytes, ever
}

const (
	// minWindow is the smallest window a Decoder refilling from a reader grows
	// to, and so the smallest read it makes of that reader.
	minWindow = 8192
	// maxVarintLen is the most bytes a 64-bit varint can occupy.
	maxVarintLen = 10
	// maxEmptyReads bounds a source that keeps returning (0, nil): io.Reader
	// permits it, and a decoder that believed it would spin forever. bufio
	// made this promise for us before the window did.
	maxEmptyReads = 100
)

var errVarintOverflow = errors.New("avro: varint overflow")

// NewDecoder wraps r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{src: r} }

// newWindowDecoder decodes bytes that are already in memory. It does not copy
// them and never reads from anywhere, so the slice must not be written to
// while the Decoder is in use — and the values handed back must not alias it,
// because the caller is free to reuse the backing array afterwards.
func newWindowDecoder(b []byte) *Decoder { return &Decoder{buf: b} }

// fill makes n unread bytes available at buf[pos:]. Callers invoke it only
// when fewer than n are there.
//
// It grows the window geometrically rather than straight to n, because n is
// often a length taken verbatim from the input: a corrupt length must not be
// able to demand its own allocation before one byte of it has been read.
func (d *Decoder) fill(n int64) error {
	if d.pos > 0 { // drop what has been consumed, keeping the rest
		d.buf = d.buf[:copy(d.buf, d.buf[d.pos:])]
		d.pos = 0
	}
	empty := 0
	for int64(len(d.buf)) < n {
		if d.src == nil {
			return d.shortErr()
		}
		if len(d.buf) == cap(d.buf) {
			size := 2 * cap(d.buf)
			if size < minWindow {
				size = minWindow
			}
			grown := make([]byte, len(d.buf), size)
			copy(grown, d.buf)
			d.buf = grown
		}
		m, err := d.src.Read(d.buf[len(d.buf):cap(d.buf)])
		d.buf = d.buf[:len(d.buf)+m]
		if err == io.EOF {
			d.src = nil // there will be no more bytes; the loop decides what that means
			continue
		}
		if err != nil {
			return err
		}
		if m == 0 {
			empty++
			if empty == maxEmptyReads {
				return io.ErrNoProgress
			}
		}
	}
	return nil
}

// shortErr tells a clean end of input from a value cut in half. Input that
// stops exactly on a value boundary is io.EOF, which ReadOCF reads as the end
// of a container; input that stops inside a value is a truncation, which is
// never a clean end of anything.
func (d *Decoder) shortErr() error {
	if d.pos == len(d.buf) {
		return io.EOF
	}
	return io.ErrUnexpectedEOF
}

// raw returns the next n bytes as a window into the Decoder's own buffer. It is
// valid only until the next read from this Decoder, so anything kept must be
// copied. n is an int64 because it can come from the input: comparing it as an
// int64 is what stops a huge length from wrapping round into a valid-looking
// slice bound.
func (d *Decoder) raw(n int64) ([]byte, error) {
	if n > int64(len(d.buf)-d.pos) {
		if err := d.fill(n); err != nil {
			return nil, err
		}
	}
	b := d.buf[d.pos : d.pos+int(n)]
	d.pos += int(n)
	return b, nil
}

// Long reads a zig-zag varint.
func (d *Decoder) Long() (int64, error) {
	// With a whole varint's worth of window in hand, no byte needs its own
	// availability check -- which is the common case inside a data block.
	if buf := d.buf[d.pos:]; len(buf) >= maxVarintLen {
		var u uint64
		for i := 0; i < maxVarintLen; i++ {
			b := buf[i]
			u |= uint64(b&0x7f) << (7 * i)
			if b&0x80 == 0 {
				d.pos += i + 1
				return int64(u>>1) ^ -int64(u&1), nil
			}
		}
		return 0, errVarintOverflow
	}
	return d.longSlow()
}

// longSlow reads a varint that may straddle the end of the window. It starts
// again from pos, which Long has not moved.
func (d *Decoder) longSlow() (int64, error) {
	var u uint64
	for shift := uint(0); ; shift += 7 {
		if d.pos == len(d.buf) {
			if err := d.fill(1); err != nil {
				return 0, err
			}
		}
		b := d.buf[d.pos]
		d.pos++
		u |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return int64(u>>1) ^ -int64(u&1), nil
		}
		if shift >= 57 { // a further 7 bits would run past 64
			return 0, errVarintOverflow
		}
	}
}

// Int reads an Avro int.
func (d *Decoder) Int() (int32, error) {
	v, err := d.Long()
	return int32(v), err
}

// Bool reads a single 0/1 byte.
func (d *Decoder) Bool() (bool, error) {
	if d.pos == len(d.buf) {
		if err := d.fill(1); err != nil {
			return false, err
		}
	}
	b := d.buf[d.pos]
	d.pos++
	return b != 0, nil
}

// span reads a length-prefixed byte run and returns it as a window into the
// Decoder's buffer — see raw for how long that stays valid.
func (d *Decoder) span() ([]byte, error) {
	n, err := d.Long()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("avro: negative length %d", n)
	}
	return d.raw(n)
}

// Blob reads a length-prefixed byte slice. The result is a copy: the Decoder's
// window is reused, and a blob that aliased it would change under its owner.
func (d *Decoder) Blob() ([]byte, error) {
	b, err := d.span()
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

// String reads a length-prefixed UTF-8 string. The conversion copies, which is
// the one allocation per string this package is willing to make: a string
// handed out over the window would be rewritten by the next record read.
func (d *Decoder) String() (string, error) {
	b, err := d.span()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Double reads an 8-byte little-endian double.
func (d *Decoder) Double() (float64, error) {
	if d.pos+8 > len(d.buf) {
		if err := d.fill(8); err != nil {
			return 0, err
		}
	}
	v := binary.LittleEndian.Uint64(d.buf[d.pos:])
	d.pos += 8
	return math.Float64frombits(v), nil
}

// Float reads a 4-byte little-endian float.
func (d *Decoder) Float() (float32, error) {
	if d.pos+4 > len(d.buf) {
		if err := d.fill(4); err != nil {
			return 0, err
		}
	}
	v := binary.LittleEndian.Uint32(d.buf[d.pos:])
	d.pos += 4
	return math.Float32frombits(v), nil
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
		// Decode as much of the block as the window holds, in one pass with one
		// growth of out, then refill and go round again. count came from the
		// input, so out is never sized to it -- only to doubles that are
		// demonstrably present.
		for count > 0 {
			ready := int64(len(d.buf)-d.pos) / 8
			if ready == 0 {
				if err := d.fill(8); err != nil {
					return nil, err
				}
				ready = int64(len(d.buf)-d.pos) / 8
			}
			n := count
			if n > ready {
				n = ready
			}
			if int64(cap(out)-len(out)) < n {
				grown := make([]float64, len(out), len(out)+int(n))
				copy(grown, out)
				out = grown
			}
			p := d.pos
			for i := int64(0); i < n; i++ {
				out = append(out, math.Float64frombits(binary.LittleEndian.Uint64(d.buf[p:])))
				p += 8
			}
			d.pos = p
			count -= n
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
