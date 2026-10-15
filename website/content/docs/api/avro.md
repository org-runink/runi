---
title: "runi/avro"
weight: 6
---

```go
import "github.com/org-runink/runi/avro"
```

Package avro is a dependency-free (standard-library-only) implementation of
the Apache Avro binary encoding and the Avro Object Container File (OCF)
format — enough of the spec to persist typed records to object storage in a
self-describing, splittable, compressed container.

# Avro is not ours

Apache Avro is a data serialisation format created and maintained by the
Apache Software Foundation, specified at https://avro.apache.org. This
package is an independent implementation of that public specification. It is
not affiliated with, endorsed by, or a product of the Apache Software
Foundation, and "Apache Avro" is their trademark, not ours. What is ours is
this Go code and its bugs.

# Why another one

The established Go implementations are good and carry dependencies. This one
exists because the rest of this module promises zero dependencies, and a
container format is not worth breaking that promise for. If you are already
using github.com/hamba/avro or github.com/linkedin/goavro, they cover more of
the specification than this does and you should keep using them.

# What is implemented, and what is not

Implemented: the binary encoding for the primitive types, records, arrays,
maps, unions and enums; OCF read and write with the null and deflate codecs;
the sync marker and block framing that make a file splittable.

NOT implemented: schema resolution between a writer's and a reader's schema,
schema registries, RPC, logical types beyond what is noted in the type table,
and the snappy and zstd codecs. If you need writer/reader schema evolution,
this package is the wrong tool — it reads a file with the schema that file
carries.

## Index

- `type` [Decoder](#decoder)
- `type` [Encoder](#encoder)
- `type` [Marshal](#marshal)
- `type` [OCFHeader](#ocfheader)
- `type` [Unmarshal](#unmarshal)
- `method` [Decoder.Blob](#decoderblob)
- `method` [Decoder.Bool](#decoderbool)
- `method` [Decoder.Double](#decoderdouble)
- `method` [Decoder.Float](#decoderfloat)
- `method` [Decoder.Float64Array](#decoderfloat64array)
- `method` [Decoder.Float64Matrix](#decoderfloat64matrix)
- `method` [Decoder.Int](#decoderint)
- `method` [Decoder.Long](#decoderlong)
- `method` [Decoder.OptionalString](#decoderoptionalstring)
- `method` [Decoder.String](#decoderstring)
- `method` [Encoder.Blob](#encoderblob)
- `method` [Encoder.Bool](#encoderbool)
- `method` [Encoder.Bytes](#encoderbytes)
- `method` [Encoder.Double](#encoderdouble)
- `method` [Encoder.Float](#encoderfloat)
- `method` [Encoder.Float64Array](#encoderfloat64array)
- `method` [Encoder.Float64Matrix](#encoderfloat64matrix)
- `method` [Encoder.Int](#encoderint)
- `method` [Encoder.Long](#encoderlong)
- `method` [Encoder.OptionalString](#encoderoptionalstring)
- `method` [Encoder.Reset](#encoderreset)
- `method` [Encoder.String](#encoderstring)
- `const` [ErrInvalid](#errinvalid)
- `func` [NewDecoder](#newdecoder)
- `func` [NewEncoder](#newencoder)
- `func` [ReadOCF](#readocf)
- `func` [ReadOCFBytes](#readocfbytes)
- `func` [WriteOCF](#writeocf)

### Decoder

```go
type Decoder struct
```

Decoder reads Avro binary primitives out of a window of bytes, refilling the
window from an io.Reader when it runs dry.

The window is the point. Decoding a value is a bounds check and an index into
a []byte, not a ReadByte or a ReadFull through an io.Reader: an indirect call
per value, plus — for every fixed-width type — a local array that escapes to
the heap because it is passed as an interface argument. That overhead, not
the arithmetic, is what made reading a container cost ten times what writing
one did. A Decoder over bytes already in memory does no I/O at all.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder

```go
type Encoder struct
```

Encoder writes Avro binary primitives to a buffer.

The zero value is an empty Encoder ready to use. Like bytes.Buffer, which it
holds, an Encoder must not be copied after first use.

<small>Defined in <code>avro/avro.go</code>.</small>

### Marshal

```go
type Marshal[T any] func(e *Encoder, v T)
```

Marshal writes one record's fields to e, in schema order.

<small>Defined in <code>avro/ocf.go</code>.</small>

### OCFHeader

```go
type OCFHeader struct
```

OCFHeader is the parsed self-describing header.

<small>Defined in <code>avro/ocf.go</code>.</small>

### Unmarshal

```go
type Unmarshal[T any] func(d *Decoder) (T, error)
```

Unmarshal reads one record's fields from d, in schema order.

<small>Defined in <code>avro/ocf.go</code>.</small>

### Decoder.Blob

```go
func (d *Decoder) Blob() ([]byte, error)
```

Blob reads a length-prefixed byte slice. The result is a copy: the Decoder's
window is reused, and a blob that aliased it would change under its owner.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Bool

```go
func (d *Decoder) Bool() (bool, error)
```

Bool reads a single 0/1 byte.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Double

```go
func (d *Decoder) Double() (float64, error)
```

Double reads an 8-byte little-endian double.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Float

```go
func (d *Decoder) Float() (float32, error)
```

Float reads a 4-byte little-endian float.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Float64Array

```go
func (d *Decoder) Float64Array() ([]float64, error)
```

Float64Array decodes an array<double> written as positive blocks + terminator.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Float64Matrix

```go
func (d *Decoder) Float64Matrix() ([][]float64, error)
```

Float64Matrix decodes an array<array<double>>.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Int

```go
func (d *Decoder) Int() (int32, error)
```

Int reads an Avro int.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.Long

```go
func (d *Decoder) Long() (int64, error)
```

Long reads a zig-zag varint.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.OptionalString

```go
func (d *Decoder) OptionalString() (s string, present bool, err error)
```

OptionalString decodes a union {null, string}. present=false means null.

<small>Defined in <code>avro/avro.go</code>.</small>

### Decoder.String

```go
func (d *Decoder) String() (string, error)
```

String reads a length-prefixed UTF-8 string. The conversion copies, which is
the one allocation per string this package is willing to make: a string
handed out over the window would be rewritten by the next record read.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Blob

```go
func (e *Encoder) Blob(b []byte)
```

Blob writes a length-prefixed byte slice (Avro bytes).

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Bool

```go
func (e *Encoder) Bool(v bool)
```

Bool writes a single 0/1 byte.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Bytes

```go
func (e *Encoder) Bytes() []byte
```

Bytes returns the accumulated encoding.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Double

```go
func (e *Encoder) Double(v float64)
```

Double writes an 8-byte little-endian IEEE-754 double.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Float

```go
func (e *Encoder) Float(v float32)
```

Float writes a 4-byte little-endian IEEE-754 float.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Float64Array

```go
func (e *Encoder) Float64Array(xs []float64)
```

Float64Array encodes an array<double> as a single positive block + terminator.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Float64Matrix

```go
func (e *Encoder) Float64Matrix(m [][]float64)
```

Float64Matrix encodes an array<array<double>> — the shape of a set of
embedding vectors — as an outer block of inner arrays + terminator.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Int

```go
func (e *Encoder) Int(v int32)
```

Int is Avro int — same wire form as long.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Long

```go
func (e *Encoder) Long(v int64)
```

Long writes a variable-length zig-zag encoded 64-bit integer (Avro long/int).

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.OptionalString

```go
func (e *Encoder) OptionalString(s string, present bool)
```

OptionalString encodes a union {null, string}: index 0 = null, 1 = value.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.Reset

```go
func (e *Encoder) Reset()
```

Reset clears the buffer for reuse.

<small>Defined in <code>avro/avro.go</code>.</small>

### Encoder.String

```go
func (e *Encoder) String(s string)
```

String writes a length-prefixed UTF-8 string.

<small>Defined in <code>avro/avro.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("avro: invalid argument")
```

ErrInvalid is wrapped by every error caused by a bad argument, such as a
codec WriteOCF does not implement. Match it with errors.Is; the message text
is not part of the API. A container that is corrupt or uses a codec this
package cannot read is a problem with the data, not an argument, and its
error does not wrap ErrInvalid.

<small>Defined in <code>avro/avro.go</code>.</small>

### NewDecoder

```go
func NewDecoder(r io.Reader) *Decoder
```

NewDecoder wraps r.

<small>Defined in <code>avro/avro.go</code>.</small>

### NewEncoder

```go
func NewEncoder() *Encoder
```

NewEncoder returns an empty Encoder. It is the same as new(Encoder).

<small>Defined in <code>avro/avro.go</code>.</small>

### ReadOCF

```go
func ReadOCF[T any](r io.Reader, unmarshal Unmarshal[T]) (OCFHeader, []T, error)
```

ReadOCF parses an OCF from r and returns the header schema/codec plus all
decoded records. It walks every data block (any block count), so it reads
files written by us or by any spec-conformant Avro writer using the same
schema and the null/deflate codecs.

<small>Defined in <code>avro/ocf.go</code>.</small>

### ReadOCFBytes

```go
func ReadOCFBytes[T any](b []byte, unmarshal Unmarshal[T]) (OCFHeader, []T, error)
```

ReadOCFBytes is ReadOCF for a container that is already in memory — which is
what an object store's GET hands you, and what this package's own writer
produces.

It decodes out of b in place instead of copying the file through a window, so
reading a few-kilobyte container costs neither the copy nor the window. The
records it returns never alias b: every string and every byte slice a Decoder
hands out is a copy, so b may be reused or returned to a pool the moment this
returns.

<small>Defined in <code>avro/ocf.go</code>.</small>

### WriteOCF

```go
func WriteOCF[T any](w io.Writer, schemaJSON, codec string, records []T, marshal Marshal[T]) error
```

WriteOCF encodes records as a single-block Avro Object Container File: the
writer's schema (JSON) is embedded in the header — the file is self-describing
— followed by the (optionally DEFLATE-compressed) record block and a sync
marker. schemaJSON must be the canonical Avro schema for T. A fresh random sync
marker is generated per file. codec must be CodecNull or CodecDeflate; any
other is refused, before anything is written, with an error wrapping
ErrInvalid.

<small>Defined in <code>avro/ocf.go</code>.</small>
