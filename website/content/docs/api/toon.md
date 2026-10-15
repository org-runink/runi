---
title: "runi/toon"
weight: 12
---

```go
import "github.com/org-runink/runi/toon"
```

Package toon reads and writes TOON, a token-oriented object notation that
models emit instead of JSON when the point is to spend fewer tokens.

TOON is indentation-structured like YAML, with two additions that are the
whole reason it exists:

name: "a scalar"
tags[3]: red,green,blue
parts[2]{sku,qty}:
  A-1,4
  B-2,9
steps:
  - label: "first"
    done: true

An array declares its length, and a uniform array of objects declares its
field names once and then writes rows. A list of ten objects with four
fields costs four field names instead of forty, which on a long reply is the
difference between fitting in a context window and not.

# Reading what a model actually sent

Decode is deliberately tolerant of the things models get wrong about their
own output, and deliberately intolerant of everything else. It accepts a
reply wrapped in a ``` fence, with or without a language tag, because models
add one about half the time. It accepts a declared length that disagrees
with the number of values, because models miscount, and the values are the
data while the count is a restatement of it — [Decode] takes the values and
[DecodeStrict] refuses the disagreement.

It does NOT guess at structure. A row with the wrong number of columns, an
indentation step that matches no open block, or a quoted string that never
closes is an error with a line number, not a repair. A repaired document is
a guess about what the model meant, and a plausible guess that parses is
worse than an honest failure — the same rule [github.com/org-runink/runi/salvage]
follows for JSON.

# Delimiters

A table's cells and an inline array's values are separated by a comma, a tab
or a pipe, declared inside the brackets of each header as the spec does --
rows[2\t]{id\tnote}: -- and absent for comma. [EncodeWith] chooses it;
[Decode] reads whatever each header declares. A value is quoted only when it
contains the active delimiter, so tab is the cheap choice for prose.

# No JSON in between

Structs, maps, slices, pointers and scalars are converted directly, with
encoding/json's naming rules and errors but without producing JSON. Types
that define their own JSON or text marshalling, embedded structs, and the
",string" and ",omitzero" options are left to encoding/json, so its rules
apply exactly.

# What this package is not

It is not a YAML parser. TOON's indentation rules are narrower: two spaces
per level, no tabs, no anchors, no multi-line scalars, no comments. Feeding
it YAML will usually fail, and when it does not, it will not mean what the
YAML meant.

It does not quote keys. A map key containing a colon, a delimiter or a
newline is written as it is and will not read back; keep such text in values.

## Index

- `type` [Options](#options)
- `type` [SyntaxError](#syntaxerror)
- `func` [Decode](#decode)
- `func` [DecodeStrict](#decodestrict)
- `func` [Encode](#encode)
- `func` [EncodeWith](#encodewith)
- `const` [ErrInvalid](#errinvalid)
- `func` [Parse](#parse)
- `method` [SyntaxError.Error](#syntaxerrorerror)

### Options

```go
type Options struct
```

Options controls how a document is written. The zero value is what [Encode]
uses.

<small>Defined in <code>toon/encode.go</code>.</small>

### SyntaxError

```go
type SyntaxError struct
```

SyntaxError says what was wrong and on which line of the input, counting
from 1 and counting the fence if there was one, so the number matches what
the caller is looking at.

<small>Defined in <code>toon/toon.go</code>.</small>

### Decode

```go
func Decode(text string, v any) error
```

Decode parses text as TOON and stores the result in v, which must be a
non-nil pointer. Field mapping follows encoding/json, including `json` tags,
so a struct that already round-trips as JSON needs no new tags, and a value
that does not fit its field is the same error json.Unmarshal would give.

Ordinary destinations -- structs, maps with string keys, slices, pointers,
scalars and `any` -- are filled straight from the parsed document; no JSON is
produced. A destination with its own UnmarshalJSON or UnmarshalText, and the
struct shapes whose rules belong to encoding/json (embedded fields, the
",string" option, Go arrays), are filled through encoding/json instead.

A declared array length that disagrees with the number of values is taken as
the model miscounting and the values win; use [DecodeStrict] to reject it.

<small>Defined in <code>toon/toon.go</code>.</small>

### DecodeStrict

```go
func DecodeStrict(text string, v any) error
```

DecodeStrict is Decode, except a declared array length that does not match the
number of values is an error. Use it when the count is load-bearing — when
it came from a schema the model was told to fill rather than from the model
counting its own output.

<small>Defined in <code>toon/toon.go</code>.</small>

### Encode

```go
func Encode(v any) (string, error)
```

Encode writes v as TOON. Field names, struct tags (`json:"name"`,
`json:",omitempty"`, `json:"-"`) and the treatment of []byte, nil slices and
maps all follow encoding/json, so a type that already round-trips as JSON
needs no new tags.

A slice of objects that all share the same keys is written in the tabular
form -- the field names once, then a row each -- because that is where TOON
saves tokens. Anything else is written as a dash list. Keys are sorted, so
the output of the same value is always the same bytes.

No JSON is produced along the way for ordinary Go values: structs, maps,
slices, pointers and scalars are walked directly. A type with its own
MarshalJSON or MarshalText -- time.Time is the common one -- and the rarer
struct shapes (embedded fields, the ",string" and ",omitzero" options) are
handed to encoding/json so its rules apply exactly; that costs speed, never
correctness.

<small>Defined in <code>toon/encode.go</code>.</small>

### EncodeWith

```go
func EncodeWith(v any, o Options) (string, error)
```

EncodeWith is [Encode] with a choice of delimiter.

<small>Defined in <code>toon/encode.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("toon: invalid option")
```

ErrInvalid is wrapped by every error from a bad option: an Options field
outside the values its doc names, such as a Delimiter other than ',', '\t'
or '|'. Match it with errors.Is; the message text is not part of the API.

<small>Defined in <code>toon/toon.go</code>.</small>

### Parse

```go
func Parse(text string) (any, error)
```

Parse returns the document as map[string]any, []any and scalars, for callers
that do not have a type to decode into.

<small>Defined in <code>toon/toon.go</code>.</small>

### SyntaxError.Error

```go
func (e *SyntaxError) Error() string
```

<small>Defined in <code>toon/toon.go</code>.</small>
