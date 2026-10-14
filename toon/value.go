// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"fmt"
	"strconv"
	"strings"
)

// keySpec is a parsed left-hand side: the name, and the array shape if the key
// declared one.
type keySpec struct {
	name    string
	isArray bool
	count   int      // declared length
	fields  []string // tabular field names, nil for a plain array
	delim   byte     // what separates values and field names: ',', '\t' or '|'
}

// splitKey reads "name", "name[3]" or "name[2]{a,b}" followed by a colon.
func splitKey(l line) (keySpec, string, error) {
	k, rest, err := parseKey(l)
	if err == nil && k.name == "" {
		err = &SyntaxError{l.n, "empty key"}
	}
	return k, rest, err
}

// parseKey is splitKey without insisting on a name, so the root array's
// keyless header can share it.
//
// The bracket may end in a delimiter, as the spec declares one: "[3|]" or
// "[3<TAB>]" -- absent means comma. Each header declares its own; nothing is
// inherited from an enclosing array.
func parseKey(l line) (keySpec, string, error) {
	t := l.text
	colon := topLevelColon(t)
	if colon < 0 {
		return keySpec{}, "", &SyntaxError{l.n, "expected a key followed by ':'"}
	}
	lhs, rest := t[:colon], strings.TrimPrefix(t[colon+1:], " ")
	k := keySpec{name: lhs, delim: ','}
	if open := strings.IndexByte(lhs, '['); open >= 0 {
		close := strings.IndexByte(lhs[open:], ']')
		if close < 0 {
			return keySpec{}, "", &SyntaxError{l.n, "array declaration is missing ']'"}
		}
		close += open
		count := lhs[open+1 : close]
		if c := count; c != "" && (c[len(c)-1] == '\t' || c[len(c)-1] == '|') {
			k.delim, count = c[len(c)-1], c[:len(c)-1]
		}
		n, err := strconv.Atoi(count)
		if err != nil || n < 0 {
			return keySpec{}, "", &SyntaxError{l.n, fmt.Sprintf("array length %q is not a count", lhs[open+1:close])}
		}
		k.name, k.isArray, k.count = lhs[:open], true, n
		if tail := lhs[close+1:]; tail != "" {
			if !strings.HasPrefix(tail, "{") || !strings.HasSuffix(tail, "}") {
				return keySpec{}, "", &SyntaxError{l.n, "expected field names in {…} after the array length"}
			}
			// Split of an empty string yields one empty element, so counting
			// the slice would always say "one field". The names themselves
			// are what must be non-empty.
			for _, f := range strings.Split(tail[1:len(tail)-1], string(k.delim)) {
				name := strings.TrimSpace(f)
				if name == "" {
					return keySpec{}, "", &SyntaxError{l.n, "tabular array has an empty field name"}
				}
				k.fields = append(k.fields, name)
			}
		}
	}
	return k, rest, nil
}

// topLevelColon finds the ':' that ends the key, ignoring any inside quotes
// and inside a {…} field list.
func topLevelColon(s string) int {
	inQuote, esc, brace := false, false, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inQuote:
			esc = true
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == '{':
			brace++
		case c == '}':
			if brace > 0 {
				brace--
			}
		case c == ':' && brace == 0:
			return i
		}
	}
	return -1
}

// value reads whatever follows a key: an inline scalar, an inline array, a
// tabular block, a dash list, or a nested mapping.
func (p *parser) value(k keySpec, rest string, depth int, at line) (any, error) {
	if k.isArray {
		return p.array(k, rest, depth, at)
	}
	if rest != "" {
		return scalar(rest, at, p.floatNums)
	}
	// Nothing on the line: a nested block, or an explicit empty value at the
	// end of the document.
	nxt, ok := p.peek()
	if !ok || nxt.indent <= depth {
		return nil, nil
	}
	if nxt.indent != depth+1 {
		return nil, &SyntaxError{nxt.n, "unexpected indentation"}
	}
	return p.block(depth + 1)
}

func (p *parser) array(k keySpec, rest string, depth int, at line) (any, error) {
	// Inline: "tags[3]: red,green,blue"
	if rest != "" {
		p.cells = splitValues(p.cells[:0], rest, k.delim)
		out := make([]any, 0, len(p.cells))
		for _, s := range p.cells {
			v, err := scalar(strings.TrimSpace(s), at, p.floatNums)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, p.checkCount(k, len(out), at)
	}
	if k.count == 0 {
		// "rules[0]:" with nothing under it. An empty result is a result.
		nxt, ok := p.peek()
		if !ok || nxt.indent <= depth {
			return []any{}, nil
		}
	}
	nxt, ok := p.peek()
	if !ok || nxt.indent <= depth {
		if err := p.checkCount(k, 0, at); err != nil {
			return nil, err
		}
		return []any{}, nil
	}
	if nxt.indent != depth+1 {
		return nil, &SyntaxError{nxt.n, "unexpected indentation"}
	}
	if k.fields != nil {
		return p.rows(k, depth+1, at)
	}
	v, err := p.block(depth + 1)
	if err != nil {
		return nil, err
	}
	items, isList := v.([]any)
	if !isList {
		return nil, &SyntaxError{nxt.n, "an array's body must be list items"}
	}
	return items, p.checkCount(k, len(items), at)
}

// rows reads the body of a tabular array: one comma-separated row per line,
// mapped onto the declared field names.
func (p *parser) rows(k keySpec, depth int, at line) (any, error) {
	out := []any{}
	for {
		l, ok := p.peek()
		if !ok || l.indent != depth {
			return out, p.checkCount(k, len(out), at)
		}
		p.cells = splitValues(p.cells[:0], l.text, k.delim)
		if len(p.cells) != len(k.fields) {
			return nil, &SyntaxError{l.n, fmt.Sprintf("row has %d values but %d fields were declared", len(p.cells), len(k.fields))}
		}
		row := make(map[string]any, len(p.cells))
		for i, f := range k.fields {
			v, err := scalar(strings.TrimSpace(p.cells[i]), l, p.floatNums)
			if err != nil {
				return nil, err
			}
			row[f] = v
		}
		out = append(out, row)
		p.i++
	}
}

// checkCount compares the declared length against what was actually there.
// Models miscount their own output constantly, and the values are the data
// while the count restates it — so Decode takes the values and only Strict
// treats the disagreement as an error.
func (p *parser) checkCount(k keySpec, got int, at line) error {
	if !p.strict || k.count == got {
		return nil
	}
	return &SyntaxError{at.n, fmt.Sprintf("%s declares %d values but has %d", k.name, k.count, got)}
}

// splitValues appends to dst the pieces of s between delimiters that are not
// inside a quoted string, so a value like "Verify, then approve" stays one
// value. The pieces are substrings of s -- quotes and escapes are left for
// scalar to read -- so splitting copies nothing.
func splitValues(dst []string, s string, delim byte) []string {
	inQuote, esc := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inQuote:
			esc = true
		case c == '"':
			inQuote = !inQuote
		case c == delim && !inQuote:
			dst = append(dst, s[start:i])
			start = i + 1
		}
	}
	return append(dst, s[start:])
}

// scalar reads one value: a quoted string, a number, a bool, null, or a bare
// string.
// floatNums makes a whole number come back as float64 rather than int64. The
// conversion still goes through ParseInt first, so the value a caller sees is
// bit-for-bit what converting the int64 afterwards produced -- this moved the
// conversion earlier, it did not change it.
func scalar(s string, at line, floatNums bool) (any, error) {
	switch {
	case s == "":
		return "", nil
	case s == "null":
		return nil, nil
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	}
	if s[0] == '"' {
		v, err := unquote(s)
		if err != nil {
			return nil, &SyntaxError{at.n, err.Error()}
		}
		return v, nil
	}
	// Text that cannot be a number is returned without asking strconv, whose
	// failed parse allocates an error carrying a copy of the input -- two per
	// cell, ParseInt then ParseFloat, for every word and path in a table.
	if !couldBeNumber(s) {
		return s, nil
	}
	if intLiteral(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			if floatNums {
				return float64(n), nil
			}
			return n, nil
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	return s, nil
}

// intLiteral reports whether s is an optional sign and then only digits: the
// complete set of strings ParseInt accepts in base 10. Anything else -- "1.5",
// "1e3" -- would only fail there, so it goes straight to ParseFloat.
func intLiteral(s string) bool {
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// unquote reads a double-quoted string with the JSON escapes. It is written
// out rather than handed to strconv.Unquote because that one also accepts Go
// syntax TOON does not have, and would turn a single-quoted rune or a
// backquoted string into a value no writer could have produced.
func unquote(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", fmt.Errorf("string is not closed")
	}
	var b strings.Builder
	body := s[1 : len(s)-1]
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", fmt.Errorf("string ends in a backslash")
		}
		switch body[i] {
		case '"', '\\', '/':
			b.WriteByte(body[i])
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 >= len(body) {
				return "", fmt.Errorf(`\u needs four hex digits`)
			}
			n, err := strconv.ParseUint(body[i+1:i+5], 16, 32)
			if err != nil {
				return "", fmt.Errorf(`\u%s is not four hex digits`, body[i+1:i+5])
			}
			b.WriteRune(rune(n))
			i += 4
		default:
			return "", fmt.Errorf("unknown escape \\%c", body[i])
		}
	}
	return b.String(), nil
}
