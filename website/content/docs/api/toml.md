---
title: "runi/toml"
weight: 13
---

```go
import "github.com/org-runink/runi/toml"
```

Package toml reads TOML v1.0.0, the configuration format of model
sampling files, tool manifests and frontmatter, into Go values.

var cfg struct {
	Models []struct {
		Name        string  `toml:"name"`
		Temperature float64 `toml:"temperature"`
		Stop        []string `toml:"stop"`
	} `toml:"models"`
}
err := toml.Unmarshal(data, &cfg)

[Unmarshal] and [Decode] fill structs, maps, slices, arrays, pointers,
interfaces and the basic types. [Parse] returns the document as a
map[string]any. A [Decoder] with [Decoder.DisallowUnknownFields] refuses
any key the destination struct has no field for, which is what strict
decoding of a configuration file needs.

# Matching keys to fields

A field's key is its `toml:"name"` tag, or its Go name when it has none.
Matching is case-sensitive, as TOML keys are: a key Temperature does not
fill a field tagged "temperature". `toml:"-"` skips a field, and options
after a comma in the tag are ignored. Fields of embedded structs are
promoted as encoding/json promotes them. Fields absent from the document
keep the value they had.

# Types

An integer fills any integer field it fits, and a float field when the
float holds it exactly; a value that does not fit is an error, never a
silent wrap or rounding. A float fills only a float field. A string fills
a string field, or any type that implements encoding.TextUnmarshaler.

An offset date-time becomes a time.Time. The three local kinds become
[LocalDateTime], [LocalDate] and [LocalTime]; a local date-time or date
may also fill a time.Time, which places it in UTC because the document
names no zone. A local time cannot fill a time.Time.

# Errors

Every problem with the document is an [*Error] carrying the line and
column, and for a decoding problem the key path with array indexes:

toml: servers[0].port: expected integer, got string (line 12:8)

Defining a key or a table twice, appending with [[ ]] to an array written
inline, and adding keys to an inline table after it closes are all
rejected, as the specification requires. The error says where the first
definition was.

# What this package is not

It does not write TOML. It reads one complete document at a time from
memory; a [Decoder] reads all of its reader first. It keeps whole seconds
from 00 to 59: TOML allows a leap second, and time.Time cannot hold one
as written, so second 60 is an error rather than a silent roll-over into
the next minute. Fractions of a second finer than a nanosecond are
truncated, which the specification permits. It implements TOML v1.0.0,
not the later v1.1 additions: times must have seconds, inline tables stay
on one line, and \e and \x escapes are errors.

It does not expand environment variables or include other files; do that
to the decoded values, where the rules are yours.

## Index

- `type` [Decoder](#decoder)
- `type` [Error](#error)
- `type` [LocalDate](#localdate)
- `type` [LocalDateTime](#localdatetime)
- `type` [LocalTime](#localtime)
- `func` [Decode](#decode)
- `method` [Decoder.Decode](#decoderdecode)
- `method` [Decoder.DisallowUnknownFields](#decoderdisallowunknownfields)
- `const` [ErrInvalid](#errinvalid)
- `method` [Error.Error](#errorerror)
- `method` [LocalDate.In](#localdatein)
- `method` [LocalDate.MarshalText](#localdatemarshaltext)
- `method` [LocalDate.String](#localdatestring)
- `method` [LocalDateTime.In](#localdatetimein)
- `method` [LocalDateTime.MarshalText](#localdatetimemarshaltext)
- `method` [LocalDateTime.String](#localdatetimestring)
- `method` [LocalTime.MarshalText](#localtimemarshaltext)
- `method` [LocalTime.String](#localtimestring)
- `func` [NewDecoder](#newdecoder)
- `func` [Parse](#parse)
- `func` [Unmarshal](#unmarshal)

### Decoder

```go
type Decoder struct
```

A Decoder reads one document from a reader.

<small>Defined in <code>toml/decode.go</code>.</small>

### Error

```go
type Error struct
```

Error is a problem with the document: a syntax error, a key or table
defined twice, a value that does not fit its Go destination, or (with
[Decoder.DisallowUnknownFields]) a key the destination has no field for.

Line and Column are 1-based; Column counts characters, not bytes. Path is
the key path the problem belongs to, with array indexes, such as
servers[0].ports; it is empty for syntax errors.

<small>Defined in <code>toml/errors.go</code>.</small>

### LocalDate

```go
type LocalDate struct
```

LocalDate is a TOML local date, such as 1979-05-27: a day on the calendar
with no time and no time zone.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDateTime

```go
type LocalDateTime struct
```

LocalDateTime is a TOML local date-time, such as 1979-05-27T07:32:00: a
moment on a wall clock, in no particular time zone.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalTime

```go
type LocalTime struct
```

LocalTime is a TOML local time, such as 07:32:00.999: a time of day with
no date and no time zone. Precision finer than a nanosecond is truncated.

<small>Defined in <code>toml/datetime.go</code>.</small>

### Decode

```go
func Decode(r io.Reader, v any) error
```

Decode reads all of r and unmarshals it into v, as [Unmarshal] does.

<small>Defined in <code>toml/decode.go</code>.</small>

### Decoder.Decode

```go
func (d *Decoder) Decode(v any) error
```

Decode reads all of the reader and unmarshals it into v.

<small>Defined in <code>toml/decode.go</code>.</small>

### Decoder.DisallowUnknownFields

```go
func (d *Decoder) DisallowUnknownFields()
```

DisallowUnknownFields makes Decode fail on any key that the destination
struct has no field for, as strict decoding of a configuration file
should: a misspelt key is then an error instead of a setting silently
left at its default. Every unknown key is reported, each as an [*Error]
with its path and position, joined into one error. Keys decoded into a
map or an interface are never unknown.

<small>Defined in <code>toml/decode.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("toml: invalid argument")
```

ErrInvalid is matched, with errors.Is, by every error caused by a bad
argument rather than by the document: a nil or non-pointer destination.

<small>Defined in <code>toml/errors.go</code>.</small>

### Error.Error

```go
func (e *Error) Error() string
```

<small>Defined in <code>toml/errors.go</code>.</small>

### LocalDate.In

```go
func (d LocalDate) In(loc *time.Location) time.Time
```

In returns midnight at the start of the date in loc.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDate.MarshalText

```go
func (d LocalDate) MarshalText() ([]byte, error)
```

MarshalText returns the date as String does, so encoding/json and other
text encoders write 1979-05-27 rather than the struct's fields.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDate.String

```go
func (d LocalDate) String() string
```

String writes the date as TOML does, YYYY-MM-DD.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDateTime.In

```go
func (dt LocalDateTime) In(loc *time.Location) time.Time
```

In returns the moment the wall-clock time names in loc.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDateTime.MarshalText

```go
func (dt LocalDateTime) MarshalText() ([]byte, error)
```

MarshalText returns the date-time as String does.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalDateTime.String

```go
func (dt LocalDateTime) String() string
```

String writes the date-time as TOML does, with a T between the parts.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalTime.MarshalText

```go
func (t LocalTime) MarshalText() ([]byte, error)
```

MarshalText returns the time as String does.

<small>Defined in <code>toml/datetime.go</code>.</small>

### LocalTime.String

```go
func (t LocalTime) String() string
```

String writes the time as HH:MM:SS, followed by the fraction of a second
without trailing zeros when there is one.

<small>Defined in <code>toml/datetime.go</code>.</small>

### NewDecoder

```go
func NewDecoder(r io.Reader) *Decoder
```

NewDecoder returns a Decoder that reads from r.

<small>Defined in <code>toml/decode.go</code>.</small>

### Parse

```go
func Parse(data []byte) (map[string]any, error)
```

Parse reads a document into a map. Values are string, int64, float64,
bool, time.Time (an offset date-time), [LocalDateTime], [LocalDate],
[LocalTime], []any and map[string]any.

<small>Defined in <code>toml/decode.go</code>.</small>

### Unmarshal

```go
func Unmarshal(data []byte, v any) error
```

Unmarshal reads a document into v, which must be a non-nil pointer. Keys
with no matching struct field are ignored; use a [Decoder] with
[Decoder.DisallowUnknownFields] to refuse them.

<small>Defined in <code>toml/decode.go</code>.</small>
