// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrInvalid is matched, with errors.Is, by every error caused by a bad
// argument rather than by the document: a nil or non-pointer destination.
var ErrInvalid = errors.New("toml: invalid argument")

// Error is a problem with the document: a syntax error, a key or table
// defined twice, a value that does not fit its Go destination, or (with
// [Decoder.DisallowUnknownFields]) a key the destination has no field for.
//
// Line and Column are 1-based; Column counts characters, not bytes. Path is
// the key path the problem belongs to, with array indexes, such as
// servers[0].ports; it is empty for syntax errors.
type Error struct {
	Line, Column int
	Path         string
	Msg          string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("toml: %s (line %d:%d)", e.Msg, e.Line, e.Column)
	}
	return fmt.Sprintf("toml: %s: %s (line %d:%d)", e.Path, e.Msg, e.Line, e.Column)
}

// errAt builds an *Error at a byte offset of data.
func errAt(data []byte, off int, format string, args ...any) *Error {
	l, c := position(data, off)
	return &Error{Line: l, Column: c, Msg: fmt.Sprintf(format, args...)}
}

// position converts a byte offset into a 1-based line and character column.
func position(data []byte, off int) (int, int) {
	before := data[:off]
	line := 1 + bytes.Count(before, []byte{'\n'})
	start := bytes.LastIndexByte(before, '\n') + 1
	return line, 1 + utf8.RuneCount(before[start:])
}
