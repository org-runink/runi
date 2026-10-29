// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These vectors were written for this package from the TOML v1.0.0
// specification, section by section. Each valid document is checked against
// the exact value Parse must return; each invalid one against the message and
// the line:column the error must point at.

type m = map[string]any
type a = []any

func TestSpecValid(t *testing.T) {
	cases := []struct {
		name, doc string
		want      m
	}{
		// Document structure, comments, whitespace, newlines.
		{"empty", "", m{}},
		{"only comments and blanks", "# a\n\n   # b\n\t\n", m{}},
		{"comment after value", "key = \"value\" # trailing # still comment\n", m{"key": "value"}},
		{"comment at end without newline", "a = 1 # c", m{"a": int64(1)}},
		{"byte order mark", "\xef\xbb\xbfa = 1", m{"a": int64(1)}},
		{"CRLF", "a = 1\r\nb = 2\r\n", m{"a": int64(1), "b": int64(2)}},
		{"no spaces and tabs", "a=1\nb\t=\t2", m{"a": int64(1), "b": int64(2)}},
		{"comment with tab and unicode", "#\tcafé ☕\na = 1", m{"a": int64(1)}},

		// Keys.
		{"bare keys", "bare_key = 1\nbare-key = 2\n1234 = 3\nABC = 4", m{"bare_key": int64(1), "bare-key": int64(2), "1234": int64(3), "ABC": int64(4)}},
		{"quoted keys", "\"127.0.0.1\" = \"v\"\n\"ʎǝʞ\" = 1\n'quoted \"value\"' = 2\n\"\" = 3\n\"a\\tb\" = 4",
			m{"127.0.0.1": "v", "ʎǝʞ": int64(1), `quoted "value"`: int64(2), "": int64(3), "a\tb": int64(4)}},
		{"dotted keys", "name = \"Orange\"\nphysical.color = \"orange\"\nsite.\"google.com\" = true",
			m{"name": "Orange", "physical": m{"color": "orange"}, "site": m{"google.com": true}}},
		{"whitespace around dots", "a . b\t.\tc = 1", m{"a": m{"b": m{"c": int64(1)}}}},
		{"dotted keys out of order", "apple.type = \"fruit\"\norange.type = \"fruit\"\napple.skin = \"thin\"",
			m{"apple": m{"type": "fruit", "skin": "thin"}, "orange": m{"type": "fruit"}}},
		{"number-looking dotted key", "3.14159 = \"pi\"", m{"3": m{"14159": "pi"}}},
		{"same name at different depths", "a = 1\nb.a = 2\n[c]\na = 3", m{"a": int64(1), "b": m{"a": int64(2)}, "c": m{"a": int64(3)}}},

		// Basic strings.
		{"basic escapes", `s = "tab\there \"q\" \\ \u00E9 \U0001F600 \b\f\n\r end"`,
			m{"s": "tab\there \"q\" \\ é 😀 \b\f\n\r end"}},
		{"basic plain", `s = "no escapes at all"`, m{"s": "no escapes at all"}},
		{"basic empty", `s = ""`, m{"s": ""}},
		{"basic with tab", "s = \"a\tb\"", m{"s": "a\tb"}},

		// Multi-line basic strings.
		{"multi-line basic trims first newline", "s = \"\"\"\nRoses are red\nViolets are blue\"\"\"", m{"s": "Roses are red\nViolets are blue"}},
		{"line-ending backslash", "s = \"\"\"\nThe quick \\\n\n\n   brown \\\n   fox.\"\"\"", m{"s": "The quick brown fox."}},
		{"line-ending backslash with trailing space and CRLF", "s = \"\"\"a \\  \t\r\n  \r\n b\"\"\"", m{"s": "a b"}},
		{"backslash at the very end", "s = \"\"\"a\\\n\"\"\"", m{"s": "a"}},
		{"quotes inside multi-line basic", `s = """Here are two quotation marks: "". Simple enough."""`, m{"s": `Here are two quotation marks: "". Simple enough.`}},
		{"content ends with quotes", `s = """"This," she said, "is just a pointless statement.""""`, m{"s": `"This," she said, "is just a pointless statement."`}},
		{"five quotes close with two", `s = """a"""""`, m{"s": `a""`}},
		{"escapes in multi-line basic", "s = \"\"\"\\u00E9\\t\\\"\\\"\\\"\n\"\"\"", m{"s": "é\t\"\"\"\n"}},
		{"CRLF kept in multi-line basic", "s = \"\"\"a\r\nb\"\"\"", m{"s": "a\r\nb"}},
		{"CRLF trimmed after opening", "s = \"\"\"\r\na\"\"\"", m{"s": "a"}},
		{"empty multi-line basic", `s = """"""`, m{"s": ""}},

		// Literal strings.
		{"literal", `w = 'C:\Users\nodejs\templates'` + "\n" + `q = '<\i\c*\s*>'` + "\n" + `t = 'Tom "Dubs" Preston-Werner'`,
			m{"w": `C:\Users\nodejs\templates`, "q": `<\i\c*\s*>`, "t": `Tom "Dubs" Preston-Werner`}},
		{"literal with tab", "s = 'a\tb'", m{"s": "a\tb"}},
		{"multi-line literal", "re = '''I [dw]on't need \\d{2} apples'''\nlines = '''\nThe first newline is\ntrimmed in raw strings.\n   All other whitespace\n   is preserved.\n'''",
			m{"re": `I [dw]on't need \d{2} apples`, "lines": "The first newline is\ntrimmed in raw strings.\n   All other whitespace\n   is preserved.\n"}},
		{"quotes inside multi-line literal", `q = ''''That,' she said, 'is still pointless.''''`, m{"q": `'That,' she said, 'is still pointless.'`}},
		{"apostrophes in multi-line literal", "s = '''Here are fifteen quotation marks: \"\"\"\"\"\"\"\"\"\"\"\"\"\"\"'''\nt = '''a''b'''", m{"s": `Here are fifteen quotation marks: """""""""""""""`, "t": "a''b"}},
		{"CRLF kept in multi-line literal", "s = '''a\r\nb\tc'''", m{"s": "a\r\nb\tc"}},
		{"SQL in a multi-line literal", "q = '''\nSELECT id, name\n  FROM users\n WHERE name LIKE 'a%' AND note = \"x\\y\"\n'''",
			m{"q": "SELECT id, name\n  FROM users\n WHERE name LIKE 'a%' AND note = \"x\\y\"\n"}},

		// Integers.
		{"integers", "a = +99\nb = 42\nc = 0\nd = -17\ne = 1_000\nf = 5_349_221\ng = 1_2_3_4_5\nh = -0\ni = +0",
			m{"a": int64(99), "b": int64(42), "c": int64(0), "d": int64(-17), "e": int64(1000), "f": int64(5349221), "g": int64(12345), "h": int64(0), "i": int64(0)}},
		{"integer limits", "max = 9223372036854775807\nmin = -9223372036854775808\nhex = 0x7FFFFFFFFFFFFFFF",
			m{"max": int64(math.MaxInt64), "min": int64(math.MinInt64), "hex": int64(math.MaxInt64)}},
		{"radix integers", "h1 = 0xDEADBEEF\nh2 = 0xdeadbeef\nh3 = 0xdead_beef\no1 = 0o01234567\no2 = 0o755\nb1 = 0b11010110\nz = 0x0",
			m{"h1": int64(0xdeadbeef), "h2": int64(0xdeadbeef), "h3": int64(0xdeadbeef), "o1": int64(0o1234567), "o2": int64(0o755), "b1": int64(0b11010110), "z": int64(0)}},

		// Floats.
		{"floats", "a = +1.0\nb = 3.1415\nc = -0.01\nd = 5e+22\ne = 1e06\nf = -2E-2\ng = 6.626e-34\nh = 224_617.445_991_228\ni = 0.0\nk = 0e0\nl = 1e1_0\nn = 1.5E+3",
			m{"a": 1.0, "b": 3.1415, "c": -0.01, "d": 5e22, "e": 1e6, "f": -0.02, "g": 6.626e-34, "h": 224617.445991228, "i": 0.0, "k": 0.0, "l": 1e10, "n": 1500.0}},
		{"infinities", "a = inf\nb = +inf\nc = -inf", m{"a": math.Inf(1), "b": math.Inf(1), "c": math.Inf(-1)}},

		// Booleans.
		{"booleans", "t = true\nf = false", m{"t": true, "f": false}},

		// Arrays.
		{"arrays", "integers = [ 1, 2, 3 ]\ncolors = [ \"red\", \"yellow\" ]\nnested = [ [ 1, 2 ], [3, 4, 5] ]\nempty = []\nspaced = [ ]",
			m{"integers": a{int64(1), int64(2), int64(3)}, "colors": a{"red", "yellow"}, "nested": a{a{int64(1), int64(2)}, a{int64(3), int64(4), int64(5)}}, "empty": a{}, "spaced": a{}}},
		{"mixed-type arrays", "mixed = [ [ 1, 2 ], [\"a\", \"b\"] ]\nstrs = [ \"all\", 'strings', \"\"\"are the same\"\"\", '''type''' ]\nnums = [ 0.1, 0.2, 1, 2 ]\nany = [1, \"a\", true, [], {}]",
			m{"mixed": a{a{int64(1), int64(2)}, a{"a", "b"}}, "strs": a{"all", "strings", "are the same", "type"}, "nums": a{0.1, 0.2, int64(1), int64(2)}, "any": a{int64(1), "a", true, a{}, m{}}}},
		{"array of inline tables", "points = [ { x = 1, y = 2 }, { x = 7, y = 8 } ]", m{"points": a{m{"x": int64(1), "y": int64(2)}, m{"x": int64(7), "y": int64(8)}}}},
		{"multi-line array with comments and trailing comma", "a = [\n  1, # one\n  2,\r\n  # nothing\n]\nb = [ # open\n]", m{"a": a{int64(1), int64(2)}, "b": a{}}},

		// Tables.
		{"tables", "[table-1]\nkey1 = \"some string\"\n[table-2]\nkey1 = \"another\"", m{"table-1": m{"key1": "some string"}, "table-2": m{"key1": "another"}}},
		{"quoted and spaced header", "[dog.\"tater.man\"]\ntype.name = \"pug\"\n[ j . \"ʞ\" . 'l' ]", m{"dog": m{"tater.man": m{"type": m{"name": "pug"}}}, "j": m{"ʞ": m{"l": m{}}}}},
		{"super-table after sub-table", "[x.y.z.w]\n[x]\nk = 1", m{"x": m{"k": int64(1), "y": m{"z": m{"w": m{}}}}}},
		{"empty table", "[a]\n[b] # c\n", m{"a": m{}, "b": m{}}},
		{"sub-table of a dotted-key table", "[fruit]\napple.color = \"red\"\napple.taste.sweet = true\n[fruit.apple.texture]\nsmooth = true",
			m{"fruit": m{"apple": m{"color": "red", "taste": m{"sweet": true}, "texture": m{"smooth": true}}}}},
		{"root dotted key then sub-table", "a.b = 1\n[a.c]\nd = 2", m{"a": m{"b": int64(1), "c": m{"d": int64(2)}}}},

		// Inline tables.
		{"inline tables", "name = { first = \"Tom\", last = \"Preston-Werner\" }\nanimal = { type.name = \"pug\", type.size = 1 }\nempty = {}\nspaced = { }\nnested = { a = { b = [1, {c = 2}] } }",
			m{"name": m{"first": "Tom", "last": "Preston-Werner"}, "animal": m{"type": m{"name": "pug", "size": int64(1)}}, "empty": m{}, "spaced": m{}, "nested": m{"a": m{"b": a{int64(1), m{"c": int64(2)}}}}}},
		{"multi-line values inside an inline table", "t = { s = \"\"\"x\ny\"\"\", a = [\n1,\n2] }", m{"t": m{"s": "x\ny", "a": a{int64(1), int64(2)}}}},

		// Arrays of tables.
		{"array of tables", "[[products]]\nname = \"Hammer\"\nsku = 738594937\n\n[[products]]  # empty table within the array\n\n[[products]]\nname = \"Nail\"\ncolor = \"gray\"",
			m{"products": a{m{"name": "Hammer", "sku": int64(738594937)}, m{}, m{"name": "Nail", "color": "gray"}}}},
		{"nested arrays of tables", "[[fruits]]\nname = \"apple\"\n[fruits.physical]\ncolor = \"red\"\n[[fruits.varieties]]\nname = \"red delicious\"\n[[fruits.varieties]]\nname = \"granny smith\"\n[[fruits]]\nname = \"banana\"\n[[fruits.varieties]]\nname = \"plantain\"",
			m{"fruits": a{
				m{"name": "apple", "physical": m{"color": "red"}, "varieties": a{m{"name": "red delicious"}, m{"name": "granny smith"}}},
				m{"name": "banana", "varieties": a{m{"name": "plantain"}}},
			}}},
		{"spaced array-of-tables header", "[[ a . b ]]\nc = 1", m{"a": m{"b": a{m{"c": int64(1)}}}}},
		{"table under array element", "[[a]]\n[a.b]\nc = 1\n[[a]]\n[a.b]\nc = 2", m{"a": a{m{"b": m{"c": int64(1)}}, m{"b": m{"c": int64(2)}}}}},
		{"super-table of an array of tables", "[[a.b]]\nx = 1\n[a]\ny = 2", m{"a": m{"b": a{m{"x": int64(1)}}, "y": int64(2)}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse([]byte(c.doc))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %#v\nwant %#v", got, c.want)
			}
		})
	}
}

// A table past a dozen keys switches from a scan to a map; both must agree.
func TestLargeTable(t *testing.T) {
	var doc strings.Builder
	want := m{}
	for i := 0; i < 30; i++ {
		k := fmt.Sprintf("k%02d", i)
		fmt.Fprintf(&doc, "%s = %d\n", k, i)
		want[k] = int64(i)
	}
	got, err := Parse([]byte(doc.String()))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v", got, err)
	}
	for _, dup := range []string{"k00", "k11", "k12", "k29"} {
		_, err := Parse([]byte(doc.String() + dup + " = 1\n"))
		if err == nil || !strings.Contains(err.Error(), "key "+dup+" is already defined") {
			t.Errorf("duplicate %s: %v", dup, err)
		}
	}
}

func TestSpecNaN(t *testing.T) {
	got, err := Parse([]byte("a = nan\nb = +nan\nc = -nan"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"a", "b", "c"} {
		if f := got[k].(float64); !math.IsNaN(f) {
			t.Errorf("%s = %v, want NaN", k, f)
		}
	}
	if !math.Signbit(got["c"].(float64)) || math.Signbit(got["a"].(float64)) {
		t.Error("the sign of nan was not kept")
	}
}

func TestSpecDateTimes(t *testing.T) {
	doc := `
odt1 = 1979-05-27T07:32:00Z
odt2 = 1979-05-27T00:32:00-07:00
odt3 = 1979-05-27T00:32:00.999999+07:30
odt4 = 1979-05-27 07:32:00z
odt5 = 1979-05-27t07:32:00.123456789123Z
ldt1 = 1979-05-27T07:32:00
ldt2 = 1979-05-27 00:32:00.999999
ld1 = 1979-05-27
leap = 2024-02-29
lt1 = 07:32:00
lt2 = 00:32:00.5
arr = [1979-05-27, 07:32:00]
`
	got, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	offset := func(k string, want string, secs int) {
		t.Helper()
		tm := got[k].(time.Time)
		if tm.Format(time.RFC3339Nano) != want {
			t.Errorf("%s = %s, want %s", k, tm.Format(time.RFC3339Nano), want)
		}
		if _, off := tm.Zone(); off != secs {
			t.Errorf("%s offset %d, want %d", k, off, secs)
		}
	}
	offset("odt1", "1979-05-27T07:32:00Z", 0)
	offset("odt2", "1979-05-27T00:32:00-07:00", -7*3600)
	offset("odt3", "1979-05-27T00:32:00.999999+07:30", 7*3600+30*60)
	offset("odt4", "1979-05-27T07:32:00Z", 0)
	offset("odt5", "1979-05-27T07:32:00.123456789Z", 0)
	want := m{
		"ldt1": LocalDateTime{LocalDate{1979, 5, 27}, LocalTime{7, 32, 0, 0}},
		"ldt2": LocalDateTime{LocalDate{1979, 5, 27}, LocalTime{0, 32, 0, 999999000}},
		"ld1":  LocalDate{1979, 5, 27},
		"leap": LocalDate{2024, 2, 29},
		"lt1":  LocalTime{7, 32, 0, 0},
		"lt2":  LocalTime{0, 32, 0, 500000000},
		"arr":  a{LocalDate{1979, 5, 27}, LocalTime{7, 32, 0, 0}},
	}
	for k, w := range want {
		if !reflect.DeepEqual(got[k], w) {
			t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
}

func TestSpecInvalid(t *testing.T) {
	cases := []struct {
		name, doc, msg string
		line, col      int
	}{
		// Encoding and structure.
		{"invalid UTF-8", "a = \"\xff\"", "not valid UTF-8 (byte 0xff)", 1, 6},
		{"lone carriage return", "a = 1\rb = 2", "carriage return must be followed by a line feed", 1, 6},
		{"two values on a line", "a = 1 b = 2", "expected the end of the line, found 'b'", 1, 7},
		{"two strings on a line", "a = \"x\" \"y\"", "expected the end of the line, found '\"'", 1, 9},
		{"column counts characters", "x = \"é\" y", "found 'y'", 1, 9},
		{"control character in comment", "# c\x01", "control character U+0001 is not allowed in a comment", 1, 4},
		{"DEL in comment", "a = 1 # \x7f", "control character U+007F", 1, 9},

		// Keys.
		{"missing key", "= 1", "expected a key, found '='", 1, 1},
		{"key ends at document end", "a", "expected '=' after the key a, found the end of the document", 1, 2},
		{"key with no value", "a = ", "expected a value, found the end of the document", 1, 5},
		{"key with no value before newline", "a = \nb = 1", "expected a value, found the end of the line", 1, 5},
		{"key and newline", "a\n= 1", "expected '=' after the key a, found the end of the line", 1, 2},
		{"dangling dot", "a. = 1", "expected a key, found '='", 1, 4},
		{"dot at end", "a.", "expected a key, found the end of the document", 1, 3},
		{"non-ASCII bare key", "é = 1", "expected a key, found 'é'", 1, 1},
		{"multi-line basic key", "\"\"\"a\"\"\" = 1", "a key cannot be a multi-line string", 1, 1},
		{"multi-line literal key", "'''a''' = 1", "a key cannot be a multi-line string", 1, 1},
		{"unclosed quoted key", "\"a = 1", "never closed", 1, 1},
		{"unclosed literal key", "'a = 1", "never closed", 1, 1},
		{"key defined twice", "a = 1\na = 2", "key a is already defined at line 1:1", 2, 1},
		{"quoted and bare are the same key", "a = 1\n'a' = 2", "key a is already defined", 2, 1},
		{"empty key defined twice", "\"\" = 1\n\"\" = 2", `key "" is already defined`, 2, 1},
		{"dotted key over a value", "a = 1\na.b = 2", "cannot add keys to a with a dotted key: it was already defined as a value", 2, 1},
		{"value over a dotted table", "a.b = 1\na = 2", "key a is already defined", 2, 1},
		{"quoted part named in message", "\"x.y\" = 1\n\"x.y\".z = 2", `cannot add keys to "x.y"`, 2, 1},

		// Strings.
		{"unclosed basic string", "a = \"abc", "this string is never closed", 1, 5},
		{"basic string across lines", "a = \"abc\nb\"", `never closed on its line; use """`, 1, 5},
		{"raw control in basic string", "a = \"\x00\"", "control character U+0000 must be escaped", 1, 6},
		{"unknown escape", `a = "\q"`, "invalid escape sequence: a backslash followed by 'q'", 1, 6},
		{"v1.1 escape \\e", `a = "\e"`, "invalid escape sequence", 1, 6},
		{"v1.1 escape \\x", `a = "\x41"`, "invalid escape sequence", 1, 6},
		{"backslash at end of document", "a = \"\\", "this string is never closed", 1, 6},
		{"short \\u", `a = "\u12"`, `\u must be followed by 4 hexadecimal digits`, 1, 6},
		{"\\u with underscore", `a = "\u1_23"`, `\u must be followed by 4 hexadecimal digits`, 1, 6},
		{"short \\U", `a = "\U0001F60"`, `\U must be followed by 8 hexadecimal digits`, 1, 6},
		{"surrogate", `a = "\uD800"`, `\uD800 is not a Unicode scalar value`, 1, 6},
		{"beyond Unicode", `a = "\U00110000"`, "is not a Unicode scalar value", 1, 6},
		{"unclosed literal", "a = 'abc", "this string is never closed", 1, 5},
		{"literal across lines", "a = 'ab\nc'", "never closed on its line; use '''", 1, 5},
		{"control in literal", "a = 'a\x01'", "control character U+0001 is not allowed in a literal string", 1, 7},
		{"unclosed multi-line basic", "a = \"\"\"abc\n", `never closed with """`, 1, 5},
		{"unclosed multi-line literal", "a = '''abc\n", "never closed with '''", 1, 5},
		{"six quotes in multi-line basic", `a = """a""""""`, "too many quotes", 1, 14},
		{"six quotes in multi-line literal", `a = '''a''''''`, "too many quotes", 1, 14},
		{"backslash space not at line end", `a = """\ x"""`, "invalid escape sequence: a backslash followed by ' '", 1, 8},
		{"lone CR in multi-line basic", "a = \"\"\"a\rb\"\"\"", "carriage return", 1, 9},
		{"lone CR after line-ending backslash", "a = \"\"\"a\\\r\"\"\"", "carriage return", 1, 10},
		{"lone CR in multi-line literal", "a = '''a\rb'''", "carriage return", 1, 9},
		{"control in multi-line basic", "a = \"\"\"\x01\"\"\"", "control character U+0001 must be escaped", 1, 8},
		{"control in multi-line literal", "a = '''\x01'''", "control character U+0001 is not allowed", 1, 8},
		{"bad escape in multi-line basic", `a = """\q"""`, "invalid escape sequence", 1, 8},

		// Integers.
		{"leading zero", "a = 01", "leading zeros are not allowed", 1, 5},
		{"leading zero with underscore", "a = 0_1", "leading zeros are not allowed", 1, 5},
		{"double underscore", "a = 1__0", "numbers are digits with single underscores", 1, 5},
		{"leading underscore", "a = _1", "strings must be quoted", 1, 5},
		{"trailing underscore", "a = 1_", "strings must be quoted", 1, 5},
		{"bare sign", "a = +", "strings must be quoted", 1, 5},
		{"too big", "a = 9223372036854775808", "does not fit in a 64-bit signed integer", 1, 5},
		{"too small", "a = -9223372036854775809", "does not fit", 1, 5},
		{"signed hex", "a = +0x1", "a hexadecimal integer cannot have a sign", 1, 5},
		{"signed octal", "a = -0o1", "an octal", 1, 5},
		{"signed binary", "a = -0b1", "binary integer cannot have a sign", 1, 5},
		{"empty hex", "a = 0x", "invalid hexadecimal digits", 1, 5},
		{"bad octal digit", "a = 0o8", "invalid octal digits", 1, 5},
		{"bad binary digit", "a = 0b2", "invalid binary digits", 1, 5},
		{"uppercase radix prefix", "a = 0X1", "strings must be quoted", 1, 5},
		{"hex underscore at start", "a = 0x_1", "invalid hexadecimal digits", 1, 5},
		{"hex beyond int64", "a = 0x8000000000000000", "does not fit in a 64-bit signed integer", 1, 5},
		{"hex beyond uint64", "a = 0x1_0000_0000_0000_0000", "does not fit", 1, 5},
		{"bare word", "a = abc", `"abc" is not a valid value: strings must be quoted`, 1, 5},
		{"capitalised boolean", "a = True", "strings must be quoted", 1, 5},
		{"capitalised inf", "a = Inf", "strings must be quoted", 1, 5},

		// Floats.
		{"trailing point", "a = 1.", "a decimal point must have digits on both sides", 1, 5},
		{"leading point", "a = .5", "strings must be quoted", 1, 5},
		{"point before exponent", "a = 1.e5", "decimal point", 1, 5},
		{"two points", "a = 0.1.2", "decimal point", 1, 5},
		{"empty exponent", "a = 1e", "an exponent needs digits", 1, 5},
		{"signed empty exponent", "a = 1e+", "an exponent needs digits", 1, 5},
		{"exponent underscore", "a = 1e_1", "an exponent needs digits", 1, 5},
		{"float leading zero", "a = 01.5", "leading zeros", 1, 5},
		{"float out of range", "a = 1e400", "out of range for a 64-bit float", 1, 5},
		{"fraction underscore at end", "a = 1.5_", "decimal point", 1, 5},

		// Dates and times.
		{"month 13", "a = 1979-13-01", "the month must be 01 to 12", 1, 5},
		{"month 0", "a = 1979-00-01", "the month must be 01 to 12", 1, 5},
		{"no February 29 in 1979", "a = 1979-02-29", "1979-02 has no day 29", 1, 5},
		{"day 0", "a = 1979-01-00", "has no day 00", 1, 5},
		{"single-digit month", "a = 1979-5-27", "expected a date as YYYY-MM-DD", 1, 5},
		{"wrong date separator", "a = 1979-05/27", "expected a date as YYYY-MM-DD", 1, 5},
		{"date then junk", "a = 1979-05-27X07:32:00", "expected a form like", 1, 5},
		{"hour 24", "a = 1979-05-27T24:00:00", "the hour must be 00 to 23", 1, 5},
		{"minute 60", "a = 07:60:00", "minutes and seconds 00 to 59", 1, 5},
		{"leap second", "a = 1979-05-27T23:59:60Z", "minutes and seconds 00 to 59", 1, 5},
		{"time without seconds", "a = 07:32", "seconds are required", 1, 5},
		{"date-time without seconds", "a = 1979-05-27T07:32Z", "seconds are required", 1, 5},
		{"time then junk", "a = 07:32:00x", "expected a form like", 1, 5},
		{"empty fraction", "a = 07:32:00.", "a decimal point in the seconds must be followed by digits", 1, 5},
		{"offset hour 24", "a = 1979-05-27T07:32:00+24:00", "the offset must be +HH:MM", 1, 5},
		{"offset not digits", "a = 1979-05-27T07:32:00+0a:00", "the offset must be +HH:MM", 1, 5},
		{"offset without colon", "a = 1979-05-27T07:32:00+0700", "the offset must be Z, +HH:MM or -HH:MM", 1, 5},
		{"offset junk", "a = 1979-05-27T07:32:00Zx", "the offset must be Z", 1, 5},
		{"time with offset only", "a = 07:32:00Z", "expected a form like", 1, 5},

		// Arrays.
		{"unclosed empty array", "a = [", "this array is never closed", 1, 5},
		{"unclosed array", "a = [1", "this array is never closed", 1, 5},
		{"unclosed array after comma", "a = [1,\n", "this array is never closed", 1, 5},
		{"missing comma", "a = [1 2]", "expected ',' or ']' after an array element, found '2'", 1, 8},
		{"double comma", "a = [1,,2]", "expected a value, found ','", 1, 8},
		{"leading comma", "a = [,]", "expected a value, found ','", 1, 6},
		{"bad comment inside array", "a = [\n# c\x01\n]", "control character", 2, 4},
		{"lone CR after array element", "a = [1\r]", "carriage return", 1, 7},
		{"lone CR before array element", "a = [\r1]", "carriage return", 1, 6},
		{"bad element", "a = [1, x]", `"x" is not a valid value`, 1, 9},

		// Inline tables.
		{"unclosed inline table at end", "a = {", "expected a key, found the end of the document", 1, 6},
		{"unclosed inline table", "a = {b = 1", "this inline table is never closed", 1, 5},
		{"trailing comma in inline table", "a = {b = 1,}", "an inline table cannot end with a comma", 1, 11},
		{"spaced trailing comma in inline table", "a = {b = 1, }", "an inline table cannot end with a comma", 1, 11},
		{"newline in inline table", "a = {b = 1\n}", "an inline table must be on a single line", 1, 11},
		{"newline before first key", "a = {\nb = 1}", "expected a key, found the end of the line", 1, 6},
		{"missing comma in inline table", "a = {b = 1 c = 2}", "expected ',' or '}' in the inline table, found 'c'", 1, 12},
		{"missing value in inline table", "a = {b = }", "expected a value, found '}'", 1, 10},
		{"key without value in inline table", "a = {b, c = 1}", "expected '=' after the key b, found ','", 1, 7},
		{"duplicate key in inline table", "a = {b = 1, b = 2}", "key b is already defined at line 1:6", 1, 13},
		{"extend nested inline table", "a = {b = {c = 1}, b.d = 2}", "cannot add keys to b with a dotted key: it was already defined as an inline table", 1, 19},
		{"dotted key into a closed inline table", "a = {b = 1}\na.c = 2", "cannot add keys to a with a dotted key: it was already defined as an inline table", 2, 1},
		{"header into a closed inline table", "a = {b = 1}\n[a.c]", "cannot define table a.c: a is already an inline table", 2, 2},
		{"header redefining an inline table", "a = {}\n[a]", "table a is already defined as an inline table", 2, 2},
		{"header into a dotted key inside an inline table", "a = {b.c = 1}\n[a.b.d]", "cannot define table a.b.d: a is already an inline table", 2, 2},

		// Tables.
		{"unclosed header", "[a", `expected "]" to close the table header, found the end of the document`, 1, 3},
		{"unclosed array-of-tables header", "[[a]", `expected "]]" to close the table header`, 1, 4},
		{"split array-of-tables closer", "[[a] ]", `expected "]]"`, 1, 4},
		{"empty header", "[]", "expected a key, found ']'", 1, 2},
		{"empty array-of-tables header", "[[]]", "expected a key, found ']'", 1, 3},
		{"split array-of-tables opener", "[ [a]]", "expected a key, found '['", 1, 3},
		{"junk after header", "[a]b", "expected the end of the line, found 'b'", 1, 4},
		{"header across lines", "[a\n]", `expected "]"`, 1, 3},
		{"table defined twice", "[a]\n[a]", "table a is already defined as a table at line 1:2", 2, 2},
		{"table defined twice via super-table", "[a.b]\n[a]\n[a]", "table a is already defined", 3, 2},
		{"header over a value", "[a]\nb = 1\n[a.b]", "table a.b is already defined as a value", 3, 4},
		{"header over a dotted-key table", "[fruit]\napple.color = \"red\"\n[fruit.apple]", "table fruit.apple is already defined as a table by dotted keys", 3, 8},
		{"header over a nested dotted-key table", "[fruit]\napple.taste.sweet = true\n[fruit.apple.taste]", "already defined as a table by dotted keys", 3, 14},
		{"header through a value", "a = 1\n[a.b]", "cannot define table a.b: a is already a value", 2, 2},
		{"header through a static array", "a = [1]\n[a.b]", "a is already an array at line 1:1", 2, 2},
		{"dotted keys into a header table", "[a.b.c]\nz = 9\n[a]\nb.c.t = 1", "cannot add keys to b with a dotted key: it was already defined as a table", 4, 1},
		{"dotted keys into an implicit table", "[a.b.c.d]\nz = 9\n[a]\nb.c.d.k.t = 1", "cannot add keys to b", 4, 1},
		{"key over a sub-table", "[a.b]\n[a]\nb = 1", "key b is already defined", 3, 1},

		// Arrays of tables.
		{"append to a static array", "a = [1]\n[[a]]", "cannot append to a with [[ ]]: it is already an array at line 1:1", 2, 3},
		{"append to an empty static array", "a = []\n[[a]]", "cannot append to a", 2, 3},
		{"append to a table", "[a]\n[[a]]", "cannot append to a with [[ ]]: it is already a table", 2, 3},
		{"table over an array of tables", "[[a]]\n[a]", "table a is already defined as an array of tables", 2, 2},
		{"key over an array of tables", "[[a.b]]\n[a]\nb = 1", "key b is already defined", 3, 1},
		{"append to an implicit table", "[a.b]\n[[a]]", "cannot append to a", 2, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.doc))
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("Parse(%q) = %v, want an *Error", c.doc, err)
			}
			if !strings.Contains(e.Msg, c.msg) {
				t.Errorf("message %q does not contain %q", e.Msg, c.msg)
			}
			if e.Line != c.line || e.Column != c.col {
				t.Errorf("at line %d:%d, want %d:%d (%v)", e.Line, e.Column, c.line, c.col, err)
			}
			if e.Path != "" {
				t.Errorf("a syntax error has path %q", e.Path)
			}
		})
	}
}

func TestErrorText(t *testing.T) {
	e := &Error{Line: 3, Column: 7, Msg: "bad"}
	if got := e.Error(); got != "toml: bad (line 3:7)" {
		t.Errorf("got %q", got)
	}
	e.Path = "servers[0].port"
	if got := e.Error(); got != "toml: servers[0].port: bad (line 3:7)" {
		t.Errorf("got %q", got)
	}
}

func TestLocalTypes(t *testing.T) {
	d := LocalDate{1979, 5, 27}
	tm := LocalTime{7, 32, 0, 0}
	frac := LocalTime{7, 32, 5, 120000000}
	dt := LocalDateTime{d, frac}
	if d.String() != "1979-05-27" || tm.String() != "07:32:00" || frac.String() != "07:32:05.12" {
		t.Errorf("got %s %s %s", d, tm, frac)
	}
	if dt.String() != "1979-05-27T07:32:05.12" {
		t.Errorf("got %s", dt)
	}
	loc := time.FixedZone("x", 3600)
	if got := d.In(loc); !got.Equal(time.Date(1979, 5, 27, 0, 0, 0, 0, loc)) {
		t.Errorf("got %v", got)
	}
	if got := dt.In(time.UTC); !got.Equal(time.Date(1979, 5, 27, 7, 32, 5, 120000000, time.UTC)) {
		t.Errorf("got %v", got)
	}
}
