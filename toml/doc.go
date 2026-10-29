// SPDX-License-Identifier: BSD-3-Clause

// Package toml reads TOML v1.0.0, the configuration format of model
// sampling files, tool manifests and frontmatter, into Go values.
//
//	var cfg struct {
//		Models []struct {
//			Name        string  `toml:"name"`
//			Temperature float64 `toml:"temperature"`
//			Stop        []string `toml:"stop"`
//		} `toml:"models"`
//	}
//	err := toml.Unmarshal(data, &cfg)
//
// [Unmarshal] and [Decode] fill structs, maps, slices, arrays, pointers,
// interfaces and the basic types. [Parse] returns the document as a
// map[string]any. A [Decoder] with [Decoder.DisallowUnknownFields] refuses
// any key the destination struct has no field for, which is what strict
// decoding of a configuration file needs.
//
// # Matching keys to fields
//
// A field's key is its `toml:"name"` tag, or its Go name when it has none.
// Matching is case-sensitive, as TOML keys are: a key Temperature does not
// fill a field tagged "temperature". `toml:"-"` skips a field, and options
// after a comma in the tag are ignored. Fields of embedded structs are
// promoted as encoding/json promotes them. Fields absent from the document
// keep the value they had.
//
// # Types
//
// An integer fills any integer field it fits, and a float field when the
// float holds it exactly; a value that does not fit is an error, never a
// silent wrap or rounding. A float fills only a float field. A string fills
// a string field, or any type that implements encoding.TextUnmarshaler.
//
// An offset date-time becomes a time.Time. The three local kinds become
// [LocalDateTime], [LocalDate] and [LocalTime]; a local date-time or date
// may also fill a time.Time, which places it in UTC because the document
// names no zone. A local time cannot fill a time.Time.
//
// # Errors
//
// Every problem with the document is an [*Error] carrying the line and
// column, and for a decoding problem the key path with array indexes:
//
//	toml: servers[0].port: expected integer, got string (line 12:8)
//
// Defining a key or a table twice, appending with [[ ]] to an array written
// inline, and adding keys to an inline table after it closes are all
// rejected, as the specification requires. The error says where the first
// definition was.
//
// # What this package is not
//
// It does not write TOML. It reads one complete document at a time from
// memory; a [Decoder] reads all of its reader first. It keeps whole seconds
// from 00 to 59: TOML allows a leap second, and time.Time cannot hold one
// as written, so second 60 is an error rather than a silent roll-over into
// the next minute. Fractions of a second finer than a nanosecond are
// truncated, which the specification permits. It implements TOML v1.0.0,
// not the later v1.1 additions: times must have seconds, inline tables stay
// on one line, and \e and \x escapes are errors.
//
// It does not expand environment variables or include other files; do that
// to the decoded values, where the rules are yours.
package toml
