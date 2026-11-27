// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// A quoted key decodes to its unquoted, unescaped text. Before v1.1.2 the
// quotes stayed in the key, so `"1": x` read back as the four-byte key `"1"`.
func TestQuotedKeyDecodesUnquoted(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]any
	}{
		{`"1": x` + "\n", map[string]any{"1": "x"}},
		{`"a:b": 2` + "\n", map[string]any{"a:b": int64(2)}},
		{`"a\"b": y` + "\n", map[string]any{`a"b`: "y"}},
		{`"a[1]": z` + "\n", map[string]any{"a[1]": "z"}},
		{`"with space": v` + "\n", map[string]any{"with space": "v"}},
		{`"k"[2]: a,b` + "\n", map[string]any{"k": []any{"a", "b"}}},
		{`"t"[1]{"x y",b}:` + "\n  1,2\n", map[string]any{"t": []any{map[string]any{"x y": int64(1), "b": int64(2)}}}},
		{"outer:\n  \"2\": inner\n", map[string]any{"outer": map[string]any{"2": "inner"}}},
		{"plain: p\n", map[string]any{"plain": "p"}},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// A quoted key that never closes, or closes with junk before the colon, is a
// syntax error, not a key with stray quote characters in it.
func TestQuotedKeyMalformedRefused(t *testing.T) {
	for _, in := range []string{
		`"open: x` + "\n",
		`"a"b: x` + "\n",
		`"bad\q": x` + "\n",
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want a syntax error", in)
		}
	}
}

// Whatever the encoder writes for a map key reads back as the same key.
func FuzzQuotedKeyRoundTrip(f *testing.F) {
	for _, s := range []string{"1", "a:b", `a"b`, "a[1]", "x y", "plain", `\`, "tab\there"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		if key == "" || strings.ContainsAny(key, "\n\r") || !utf8.ValidString(key) {
			t.Skip()
		}
		in := quote(key) + ": v\n"
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		m, ok := got.(map[string]any)
		if !ok || len(m) != 1 || m[key] != "v" {
			t.Fatalf("Parse(%q) = %#v, want key %q", in, got, key)
		}
	})
}

// A quoted field name in a tabular header goes through the same unquoting as a quoted key, and a bad
// escape in it is a syntax error, not a field named with stray backslashes.
func TestQuotedFieldNameMalformedRefused(t *testing.T) {
	_, err := Parse(`"t"[1]{"bad\q",b}:` + "\n  1,2\n")
	if err == nil {
		t.Fatal("a tabular header with a bad escape in a quoted field name must be a syntax error")
	}
	if !strings.Contains(err.Error(), "quoted field name") {
		t.Errorf("error = %v, want it to name the quoted field name", err)
	}
}

// closingQuote reports -1 for a string whose quote never closes, and the index of the unescaped
// closing quote otherwise; an escaped quote does not close it.
func TestClosingQuote(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{`"open`, -1},
		{`"a\"`, -1},
		{`"a"`, 2},
		{`"a\"b"`, 5},
	} {
		if got := closingQuote(c.in); got != c.want {
			t.Errorf("closingQuote(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
