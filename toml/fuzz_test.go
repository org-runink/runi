// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"errors"
	"testing"
)

// FuzzParse holds two properties on any input: Parse never panics, and every
// failure is an *Error whose position lies inside the document.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "a = 1", "[a]\nb = 'c'", "[[a]]\n[a.b]\nc = [1, {d = 2}]",
		"s = \"\"\"\\\n x\"\"\"", "t = 1979-05-27T07:32:00Z", "a.b.c = 0x_1", "x = {y = 1,}",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, err := Parse(data)
		if err == nil {
			return
		}
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("Parse(%q) returned %T, not *Error", data, err)
		}
		if e.Line < 1 || e.Column < 1 || e.Line > 1+countLines(data) {
			t.Fatalf("Parse(%q): position %d:%d is outside the document", data, e.Line, e.Column)
		}
	})
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
