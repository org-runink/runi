---
title: "runi/salvage"
weight: 5
---

```go
import "github.com/org-runink/runi/salvage"
```

Package salvage gets JSON out of text that was supposed to be JSON and is
not quite: a language model's reply.

Ask a model for a JSON object and you will mostly get one, wrapped in
something: a markdown code fence, a sentence of preamble ("Sure! Here is the
analysis:"), a closing remark, two objects where you asked for one, or an
array where you asked for an object. encoding/json rightly refuses all of
it. The usual fix is a regular expression from the first "{" to the last
"}", which breaks as soon as the prose contains a brace, a string contains
one, or the reply holds two values.

This package scans instead of matching. It walks the text once, honouring
JSON string literals and escapes, and yields every top-level balanced object
or array in order:

var a Analysis
if err := salvage.Decode(reply, &a); err != nil {
	// nothing in the reply decoded as an Analysis
}

Decode tries the whole text first, then each candidate in order, and stops
at the first one that decodes into the destination with no trailing data.
Candidates returns the raw values if you want to choose yourself.

# What it does not do

It extracts; it never repairs. A trailing comma, single-quoted strings,
unquoted keys, comments, NaN, or a reply cut off mid-object are not fixed,
and such a value is skipped. A "repaired" document is a guess about what the
model meant, and a plausible guess that decodes is worse than an honest
failure. If you need repair, ask the model again.

A candidate that decodes is only well-formed, not correct: Decode does not
check that required fields are present or values are sensible. Validate what
you decoded. By default unknown fields are ignored, as encoding/json does;
use DecodeStrict to treat a value with unknown fields as not a match.

# Speed

The reply this package was written for -- prose around one flat JSON object,
read into a struct of ordinary fields -- is decoded in a single pass that
does not go through encoding/json at all. That path is a shortcut, never a
second opinion: anything it is not certain of, from an escape in a string to
a type with an UnmarshalJSON of its own, it declines and hands back, so what
decodes, and what it decodes to, is the same either way. The tests hold it
to that by running generated replies and a fuzzer through both.

Bound the input yourself. The scan is linear for ordinary replies, but text
with many brackets that never close makes it quadratic, and Decode parses
each candidate it tries until one fits. On success the destination is
replaced, not merged into: a failed attempt never leaves it partly filled.

## Index

- `type` [Value](#value)
- `func` [Candidates](#candidates)
- `func` [Decode](#decode)
- `func` [DecodeOne](#decodeone)
- `func` [DecodeStrict](#decodestrict)
- `const` [ErrAmbiguous](#errambiguous)
- `const` [ErrNoJSON](#errnojson)
- `const` [ErrTruncated](#errtruncated)
- `func` [First](#first)
- `func` [Scan](#scan)

### Value

```go
type Value struct
```

Value is one balanced candidate and where it was found, so a caller can
report a position or slice the surrounding text.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### Candidates

```go
func Candidates(text string) []string
```

Candidates returns every top-level balanced JSON object or array in text, in
order of appearance. A value nested inside another is part of its enclosing
value, not a candidate of its own, and an opening bracket that is never
closed yields nothing — not even the values inside it. See Scan if you need
to know that the text was cut off.

A candidate is balanced, not necessarily valid JSON: Decode checks validity.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### Decode

```go
func Decode(text string, v any) error
```

Decode stores in v the first JSON value in text that decodes into it: the
whole text if it is one JSON value, otherwise each of Candidates in order.
Unknown object fields are ignored, as with json.Unmarshal. It returns
ErrNoJSON if nothing decodes.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### DecodeOne

```go
func DecodeOne(text string, v any) error
```

DecodeOne is Decode for callers who need the reply to be unambiguous, and it
fails closed. It returns ErrTruncated if the text was cut off, ErrAmbiguous
if more than one value decodes, and ErrNoJSON if none does.

This is the one to reach for when the decoded value drives a decision rather
than being shown to someone. A model asked for a verdict and cut off at a
token limit, or answering twice, is not a verdict; Decode would hand back the
first thing that fits, which is how a truncated review becomes an empty list
of findings and reads as approval. Nine call sites that each check those two
conditions by hand is the same mistake written nine times, so it is written
once, here.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### DecodeStrict

```go
func DecodeStrict(text string, v any) error
```

DecodeStrict is Decode, except a value carrying fields the destination does
not have is not a match. Use it when the shape itself is the signal, so a
different object in the same reply is not mistaken for the one you asked for.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### ErrAmbiguous

```go
var ErrAmbiguous = errors.New("salvage: more than one value decodes into the destination")
```

ErrAmbiguous reports that more than one value in the text decoded into the
destination, so which one was meant is a guess.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### ErrNoJSON

```go
var ErrNoJSON = errors.New("salvage: no JSON value in the text decoded into the destination")
```

ErrNoJSON reports that no value in the text decoded into the destination.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### ErrTruncated

```go
var ErrTruncated = errors.New("salvage: the text is truncated")
```

ErrTruncated reports that the text was cut off: a bracket, or a string, was
opened and never closed.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### First

```go
func First(text string) (string, bool)
```

First returns the first candidate that is valid JSON, and false if there is
none. It does not consider the text as a whole; use Decode for that.

<small>Defined in <code>salvage/salvage.go</code>.</small>

### Scan

```go
func Scan(text string) (vals []Value, truncated bool)
```

Scan returns every top-level balanced value in text, and whether the text was
truncated: a bracket was opened and never closed.

Truncation is reported because it is the one failure that silently produces a
WRONG answer rather than no answer. A reply cut off at a token limit leaves
its outer object unclosed while the objects nested inside it are complete, so
treating those as top-level hands back a fragment that decodes cleanly and
means something entirely different from the whole. The case that matters: a
reviewer asked for {"findings": [...]} is cut off mid-list, the first finding
decodes on its own, its "findings" key is absent, and the caller reads zero
findings from a reply that was reporting several. Scan yields nothing for the
unclosed region and sets truncated, so a caller can fail closed.

Scan is a single pass that honours JSON string literals and escapes and
matches brackets by type, so "{"a": [1, 2]}" is one value and a "]" inside an
object does not close it.

<small>Defined in <code>salvage/salvage.go</code>.</small>
