// SPDX-License-Identifier: BSD-3-Clause

// Package toon reads and writes TOON, a token-oriented object notation that
// models emit instead of JSON when the point is to spend fewer tokens.
//
// TOON is indentation-structured like YAML, with two additions that are the
// whole reason it exists:
//
//	name: "a scalar"
//	tags[3]: red,green,blue
//	parts[2]{sku,qty}:
//	  A-1,4
//	  B-2,9
//	steps:
//	  - label: "first"
//	    done: true
//
// An array declares its length, and a uniform array of objects declares its
// field names once and then writes rows. A list of ten objects with four
// fields costs four field names instead of forty, which on a long reply is the
// difference between fitting in a context window and not.
//
// # Reading what a model actually sent
//
// Decode is deliberately tolerant of the things models get wrong about their
// own output, and deliberately intolerant of everything else. It accepts a
// reply wrapped in a ``` fence, with or without a language tag, because models
// add one about half the time. It accepts a declared length that disagrees
// with the number of values, because models miscount, and the values are the
// data while the count is a restatement of it — [Decode] takes the values and
// [DecodeStrict] refuses the disagreement.
//
// It does NOT guess at structure. A row with the wrong number of columns, an
// indentation step that matches no open block, or a quoted string that never
// closes is an error with a line number, not a repair. A repaired document is
// a guess about what the model meant, and a plausible guess that parses is
// worse than an honest failure — the same rule [github.com/org-runink/runi/salvage]
// follows for JSON.
//
// # Delimiters
//
// A table's cells and an inline array's values are separated by a comma, a tab
// or a pipe, declared inside the brackets of each header as the spec does --
// rows[2\t]{id\tnote}: -- and absent for comma. [EncodeWith] chooses it;
// [Decode] reads whatever each header declares. A value is quoted only when it
// contains the active delimiter, so tab is the cheap choice for prose.
//
// # No JSON in between
//
// Structs, maps, slices, pointers and scalars are converted directly, with
// encoding/json's naming rules and errors but without producing JSON. Types
// that define their own JSON or text marshalling, embedded structs, and the
// ",string" and ",omitzero" options are left to encoding/json, so its rules
// apply exactly.
//
// # What this package is not
//
// It is not a YAML parser. TOON's indentation rules are narrower: two spaces
// per level, no tabs, no anchors, no multi-line scalars, no comments. Feeding
// it YAML will usually fail, and when it does not, it will not mean what the
// YAML meant.
//
// It does not quote keys. A map key containing a colon, a delimiter or a
// newline is written as it is and will not read back; keep such text in values.
package toon
