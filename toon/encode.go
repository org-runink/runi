// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Encode writes v as TOON. v is first marshalled as JSON, so the shape and the
// field names are exactly what encoding/json would produce, including tags.
//
// A slice of objects that all share the same keys is written in the tabular
// form — the field names once, then a row each — because that is where TOON
// saves tokens. Anything else is written as a dash list. Map keys are sorted,
// so the output of the same value is always the same bytes.
func Encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("toon: %w", err)
	}
	doc, err := jsonToValue(b)
	if err != nil {
		return "", fmt.Errorf("toon: %w", err)
	}
	if err := checkEncodable(doc, false); err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := writeValue(&sb, doc, 0); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// checkEncodable refuses the one shape the format cannot carry back unchanged,
// before anything is written.
//
// An empty list directly inside a list has no representation. A nested list is
// written as a bare dash followed by its items one level in, and with no items
// there is nothing to follow the dash -- which the parser reads as an empty
// OBJECT, because a bare dash opens a mapping unless its body says otherwise.
// Encoding it anyway would turn [[]] into [{}], so it is refused with a message
// that says what to do instead. Every other nesting round-trips.
func checkEncodable(v any, inList bool) error {
	switch t := v.(type) {
	case []any:
		if inList && len(t) == 0 {
			return errors.New("toon: an empty list inside a list has no TOON representation, " +
				"because a dash with nothing after it reads back as an empty object; " +
				"wrap it in an object, or drop it")
		}
		for _, it := range t {
			if err := checkEncodable(it, true); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, it := range t {
			if err := checkEncodable(it, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeValue(sb *strings.Builder, v any, depth int) error {
	switch t := v.(type) {
	case map[string]any:
		writeMap(sb, t, depth)
		return nil
	case []any:
		writeList(sb, "", t, depth)
		return nil
	default:
		// A scalar on its own is not a TOON document: the format's top level
		// is a mapping or a list, and a bare value has nowhere to attach.
		return fmt.Errorf("toon: a document must be an object or an array, not %T", v)
	}
}

func writeMap(sb *strings.Builder, m map[string]any, depth int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeField(sb, k, m[k], depth)
	}
}

func writeField(sb *strings.Builder, key string, v any, depth int) {
	pad := strings.Repeat(" ", depth*indentWidth)
	switch t := v.(type) {
	case []any:
		writeList(sb, key, t, depth)
	case map[string]any:
		fmt.Fprintf(sb, "%s%s:\n", pad, key)
		writeMap(sb, t, depth+1)
	default:
		fmt.Fprintf(sb, "%s%s: %s\n", pad, key, formatScalar(v))
	}
}

func writeList(sb *strings.Builder, key string, items []any, depth int) {
	pad := strings.Repeat(" ", depth*indentWidth)
	if key == "" {
		key = "items"
	}
	if len(items) == 0 {
		fmt.Fprintf(sb, "%s%s[0]:\n", pad, key)
		return
	}
	if fields, ok := tabular(items); ok {
		fmt.Fprintf(sb, "%s%s[%d]{%s}:\n", pad, key, len(items), strings.Join(fields, ","))
		inner := strings.Repeat(" ", (depth+1)*indentWidth)
		for _, it := range items {
			row := it.(map[string]any)
			cells := make([]string, len(fields))
			for i, f := range fields {
				cells[i] = formatScalar(row[f])
			}
			fmt.Fprintf(sb, "%s%s\n", inner, strings.Join(cells, ","))
		}
		return
	}
	if scalars, ok := allScalars(items); ok {
		fmt.Fprintf(sb, "%s%s[%d]: %s\n", pad, key, len(items), strings.Join(scalars, ","))
		return
	}
	fmt.Fprintf(sb, "%s%s[%d]:\n", pad, key, len(items))
	writeDashItems(sb, items, depth+1)
}

// writeDashItems writes items as "- " entries at depth, with no key header, so
// it can be used both under a "key[n]:" line and under a bare dash.
func writeDashItems(sb *strings.Builder, items []any, depth int) {
	inner := strings.Repeat(" ", depth*indentWidth)
	for _, it := range items {
		switch t := it.(type) {
		case []any:
			// A list directly inside a list. A bare dash opens the item and
			// the inner list is written one level in, which block() reads back
			// as a nested list because its first line starts with "- ".
			//
			// Without this case a slice fell through to formatScalar, which
			// has no way to render one and wrote Go's %v of it inside quotes:
			// [[1]] encoded as `- "[1]"` and decoded back as the STRING
			// "[1]". The structure was silently replaced by a description of
			// it, and the only test covering list-of-lists asserted that the
			// result decoded, never that the value survived.
			fmt.Fprintf(sb, "%s-\n", inner)
			writeDashItems(sb, t, depth+1)
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				fmt.Fprintf(sb, "%s-\n", inner)
				continue
			}
			// Every field of the item is rendered at the item's own depth, and
			// the dash replaces the indent of the very first line. Rendering
			// the first field at depth 0 instead would put a nested object's
			// body at the wrong level: the key would sit after the dash but
			// its contents would be written as if the item were the document.
			for i, k := range keys {
				var one strings.Builder
				writeField(&one, k, t[k], depth+1)
				text := one.String()
				if i == 0 {
					text = inner + "- " + strings.TrimLeft(text, " ")
				}
				sb.WriteString(text)
			}
		default:
			fmt.Fprintf(sb, "%s- %s\n", inner, formatScalar(it))
		}
	}
}

// tabular reports whether every item is an object with the same keys, which is
// the shape the row form exists for.
func tabular(items []any) ([]string, bool) {
	first, ok := items[0].(map[string]any)
	if !ok || len(first) == 0 {
		return nil, false
	}
	fields := make([]string, 0, len(first))
	for k, v := range first {
		if !isScalar(v) {
			return nil, false // a nested value has no cell to live in
		}
		fields = append(fields, k)
	}
	sort.Strings(fields)
	for _, it := range items[1:] {
		m, ok := it.(map[string]any)
		if !ok || len(m) != len(fields) {
			return nil, false
		}
		for _, f := range fields {
			v, present := m[f]
			if !present || !isScalar(v) {
				return nil, false
			}
		}
	}
	return fields, true
}

func allScalars(items []any) ([]string, bool) {
	out := make([]string, len(items))
	for i, it := range items {
		if !isScalar(it) {
			return nil, false
		}
		out[i] = formatScalar(it)
	}
	return out, true
}

func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// formatScalar quotes anything that would otherwise be read back as something
// else: a number, a bool, null, or a string carrying a comma, a quote, a
// colon, a newline or edge whitespace.
func formatScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case string:
		if needsQuote(t) {
			return quote(t)
		}
		return t
	default:
		return quote(fmt.Sprint(t))
	}
}

func needsQuote(s string) bool {
	if s == "" || s == "null" || s == "true" || s == "false" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	if strings.ContainsAny(s, ",\":\n\r\t\\[]{}") {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true // would read back as a number
	}
	return strings.HasPrefix(s, "- ")
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsonToValue re-reads marshalled JSON as a generic value, keeping numbers as
// json.Number so 1 stays "1" rather than becoming "1e+00". It is a variable
// because its error cannot happen — the bytes came from json.Marshal one line
// earlier — and an error that is returned but never run is not known to work.
var jsonToValue = func(b []byte) (any, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}
