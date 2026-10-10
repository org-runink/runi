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
	// A document that is ALREADY maps, slices and scalars is written straight
	// out. The round trip below exists to honour struct tags, omitempty and
	// custom marshalers by letting encoding/json decide the shape -- but when
	// the caller hands over a generic tree there is nothing left for it to
	// decide, and marshalling the whole document to JSON only to parse it back
	// was costing more than writing the TOON. On a 2,000-row table that was
	// about 70% of Encode's time and 12x the cost of json.Marshal itself.
	doc, direct := genericTree(v)
	if !direct {
		b, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("toon: %w", err)
		}
		doc, err = jsonToValue(b)
		if err != nil {
			return "", fmt.Errorf("toon: %w", err)
		}
	}
	return encodeGeneric(doc)
}

// encodeGeneric writes an already-generic document. Encode reaches it by two
// routes -- directly, or after the marshal-and-reparse -- and both must
// produce the same bytes; TestPropertyFastPathMatchesTheRoundTrip checks that.
func encodeGeneric(doc any) (string, error) {
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

// genericTree reports whether v is already a tree of the kinds TOON writes, so
// the marshal-and-reparse round trip can be skipped. It allocates nothing: it
// walks the value and answers.
//
// Anything else -- a struct, a named map type, a typed slice -- goes the long
// way, because only encoding/json knows what its tags mean.
func genericTree(v any) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		for _, e := range t {
			if _, ok := genericTree(e); !ok {
				return nil, false
			}
		}
		return t, true
	case []any:
		for _, e := range t {
			if _, ok := genericTree(e); !ok {
				return nil, false
			}
		}
		return t, true
	case nil, bool, string, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v, true
	default:
		return nil, false
	}
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

// writeInt appends a number without allocating.
//
// The digits are formed by hand rather than with strconv.AppendInt, because
// passing the scratch slice to AppendInt makes the array escape to the heap --
// one allocation per number, which on a table of 2,000 rows with two numeric
// columns is 4,000 of them. Ranging over the array and writing bytes keeps it
// on the stack.
func writeInt(sb *strings.Builder, v int64) {
	if v == 0 {
		sb.WriteByte('0')
		return
	}
	neg := v < 0
	u := uint64(v)
	if neg {
		u = uint64(-v)
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	for _, c := range buf[i:] {
		sb.WriteByte(c)
	}
}

// writeScalar is formatScalar writing into the builder instead of returning a
// string. The kinds a table is actually made of are handled without allocating;
// anything rarer falls back to formatScalar so there is exactly one definition
// of how a value renders.
func writeScalar(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		writeInt(sb, int64(t))
	case int8:
		writeInt(sb, int64(t))
	case int16:
		writeInt(sb, int64(t))
	case int32:
		writeInt(sb, int64(t))
	case int64:
		writeInt(sb, t)
	case string:
		if needsQuote(t) {
			writeQuoted(sb, t)
			return
		}
		sb.WriteString(t)
	default:
		sb.WriteString(formatScalar(v))
	}
}

// writeQuoted is quote() writing in place, with no intermediate Builder.
func writeQuoted(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
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
		// Written straight into the builder. The obvious version formats each
		// cell into its own string, collects them in a slice and Joins it, per
		// row -- which for a 2,000-row table is a slice, four strings, a join
		// and an Fprintf each time, and was most of Encode's allocations.
		sb.WriteString(pad)
		sb.WriteString(key)
		sb.WriteByte('[')
		writeInt(sb, int64(len(items)))
		sb.WriteString("]{")
		for i, f := range fields {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(f)
		}
		sb.WriteString("}:\n")
		inner := strings.Repeat(" ", (depth+1)*indentWidth)
		for _, it := range items {
			row := it.(map[string]any)
			sb.WriteString(inner)
			for i, f := range fields {
				if i > 0 {
					sb.WriteByte(',')
				}
				writeScalar(sb, row[f])
			}
			sb.WriteByte('\n')
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
	case int:
		return strconv.FormatInt(int64(t), 10)
	case int8:
		return strconv.FormatInt(int64(t), 10)
	case int16:
		return strconv.FormatInt(int64(t), 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint:
		return strconv.FormatUint(uint64(t), 10)
	case uint8:
		return strconv.FormatUint(uint64(t), 10)
	case uint16:
		return strconv.FormatUint(uint64(t), 10)
	case uint32:
		return strconv.FormatUint(uint64(t), 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case float32, float64:
		// Deferred to encoding/json so a float reads back byte for byte the
		// same as it would have through the round trip: Go's JSON encoder
		// switches to exponent form at magnitudes strconv would not.
		b, err := json.Marshal(t)
		if err != nil {
			return quote(fmt.Sprint(t))
		}
		return string(b)
	case string:
		if needsQuote(t) {
			return quote(t)
		}
		return t
	default:
		return quote(fmt.Sprint(t))
	}
}

// couldBeNumber reports whether s even begins like a number. It is a cheap
// filter in front of strconv, never a parser: it may say yes to something that
// is not a number, and the parse then settles it. It must never say no to
// something that IS one, which is what TestPropertyCouldBeNumberNeverMissesOne
// checks against strconv itself.
func couldBeNumber(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c == '+' || c == '-' || c == '.' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	// Infinity and NaN are numbers to ParseFloat, but only in their exact
	// spellings. Accepting every string that merely STARTS with i or n sent
	// each one to the parser -- and in a table of file paths that is most of
	// them, which was the whole allocation this filter exists to avoid.
	switch c {
	case 'i', 'I', 'n', 'N':
		return isInfOrNaN(s)
	}
	return false
}

// isInfOrNaN reports whether s is one of the unsigned spellings ParseFloat
// accepts. The signed forms are already covered: they start with + or -.
func isInfOrNaN(s string) bool {
	switch len(s) {
	case 3:
		return strings.EqualFold(s, "inf") || strings.EqualFold(s, "nan")
	case 8:
		return strings.EqualFold(s, "infinity")
	}
	return false
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
	// Only ask strconv when the string could plausibly be a number. A failed
	// ParseFloat allocates a *NumError with a copy of the string inside it, and
	// for ordinary text -- a severity, a file path -- the parse was always
	// going to fail. On a 2,000-row table those discarded errors were 98% of
	// Encode's allocations.
	if couldBeNumber(s) {
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return true // would read back as a number
		}
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
