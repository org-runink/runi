// SPDX-License-Identifier: BSD-3-Clause

// Package salvage gets JSON out of text that was supposed to be JSON and is
// not quite: a language model's reply.
//
// Ask a model for a JSON object and you will mostly get one, wrapped in
// something: a markdown code fence, a sentence of preamble ("Sure! Here is the
// analysis:"), a closing remark, two objects where you asked for one, or an
// array where you asked for an object. encoding/json rightly refuses all of
// it. The usual fix is a regular expression from the first "{" to the last
// "}", which breaks as soon as the prose contains a brace, a string contains
// one, or the reply holds two values.
//
// This package scans instead of matching. It walks the text once, honouring
// JSON string literals and escapes, and yields every top-level balanced object
// or array in order:
//
//	var a Analysis
//	if err := salvage.Decode(reply, &a); err != nil {
//		// nothing in the reply decoded as an Analysis
//	}
//
// Decode tries the whole text first, then each candidate in order, and stops
// at the first one that decodes into the destination with no trailing data.
// Candidates returns the raw values if you want to choose yourself.
//
// # What it does not do
//
// It extracts; it never repairs. A trailing comma, single-quoted strings,
// unquoted keys, comments, NaN, or a reply cut off mid-object are not fixed,
// and such a value is skipped. A "repaired" document is a guess about what the
// model meant, and a plausible guess that decodes is worse than an honest
// failure. If you need repair, ask the model again.
//
// A candidate that decodes is only well-formed, not correct: Decode does not
// check that required fields are present or values are sensible. Validate what
// you decoded. By default unknown fields are ignored, as encoding/json does;
// use DecodeStrict to treat a value with unknown fields as not a match.
//
// # Speed
//
// The reply this package was written for -- prose around one flat JSON object,
// read into a struct of ordinary fields -- is decoded in a single pass that
// does not go through encoding/json at all. That path is a shortcut, never a
// second opinion: anything it is not certain of, from an escape in a string to
// a type with an UnmarshalJSON of its own, it declines and hands back, so what
// decodes, and what it decodes to, is the same either way. The tests hold it
// to that by running generated replies and a fuzzer through both.
//
// Bound the input yourself. The scan is linear for ordinary replies, but text
// with many brackets that never close makes it quadratic, and Decode parses
// each candidate it tries until one fits. On success the destination is
// replaced, not merged into: a failed attempt never leaves it partly filled.
package salvage
