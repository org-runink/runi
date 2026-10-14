// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Options controls how a document is written. The zero value is what [Encode]
// uses.
type Options struct {
	// Delimiter separates the values of an inline array and the cells of a
	// table row. It is one of ',' (the default, also chosen by the zero value),
	// '\t' or '|', and it is declared in every array header -- items[2|]{a|b}
	// -- so any TOON reader knows how to split the rows without being told.
	//
	// Tab is usually the cheapest. A value is quoted when it contains the
	// active delimiter, so with commas as the delimiter every cell holding
	// prose ("Verify, then approve") pays for two quote characters; with tabs
	// it pays for nothing, because text almost never contains a tab. A tab is
	// also a single token in the common tokenizers, where ',"' sequences are
	// often split.
	Delimiter rune
}

// Encode writes v as TOON. Field names, struct tags (`json:"name"`,
// `json:",omitempty"`, `json:"-"`) and the treatment of []byte, nil slices and
// maps all follow encoding/json, so a type that already round-trips as JSON
// needs no new tags.
//
// A slice of objects that all share the same keys is written in the tabular
// form -- the field names once, then a row each -- because that is where TOON
// saves tokens. Anything else is written as a dash list. Keys are sorted, so
// the output of the same value is always the same bytes.
//
// No JSON is produced along the way for ordinary Go values: structs, maps,
// slices, pointers and scalars are walked directly. A type with its own
// MarshalJSON or MarshalText -- time.Time is the common one -- and the rarer
// struct shapes (embedded fields, the ",string" and ",omitzero" options) are
// handed to encoding/json so its rules apply exactly; that costs speed, never
// correctness.
func Encode(v any) (string, error) { return EncodeWith(v, Options{}) }

// EncodeWith is [Encode] with a choice of delimiter.
func EncodeWith(v any, o Options) (string, error) {
	delim, err := o.delim()
	if err != nil {
		return "", err
	}
	// A document that is ALREADY maps, slices and scalars is written straight
	// out. Anything else is first converted to that shape by walking it with
	// reflection, which applies encoding/json's naming rules without producing
	// JSON; only the shapes that walk declines go through json.Marshal.
	doc, direct := genericTree(v)
	if !direct {
		doc, err = toTree(reflect.ValueOf(v), 0)
		if errors.Is(err, errNeedsJSON) {
			doc, err = viaJSON(v)
		}
		if err != nil {
			return "", err
		}
	}
	return encodeGeneric(doc, delim)
}

// delim validates the option and turns it into the byte the writer uses.
func (o Options) delim() (byte, error) {
	switch o.Delimiter {
	case 0, ',':
		return ',', nil
	case '\t':
		return '\t', nil
	case '|':
		return '|', nil
	}
	return 0, fmt.Errorf("toon: delimiter %q is not one TOON declares; use ',', '\\t' or '|'", o.Delimiter)
}

// viaJSON is the route for the shapes toTree declines: marshal, then re-read
// as a generic tree with numbers kept as json.Number so 1 stays "1".
func viaJSON(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("toon: %w", err)
	}
	doc, err := jsonToValue(b)
	if err != nil {
		return nil, fmt.Errorf("toon: %w", err)
	}
	return doc, nil
}

// encodeGeneric writes an already-generic document. Encode reaches it by three
// routes -- directly, through toTree, or after the marshal-and-reparse -- and
// all must produce the same bytes; the FastPath and Reflect property tests
// check that.
func encodeGeneric(doc any, delim byte) (string, error) {
	if err := checkEncodable(doc, false); err != nil {
		return "", err
	}
	e := &encoder{delim: delim}
	if err := e.writeValue(doc, 0); err != nil {
		return "", err
	}
	return e.sb.String(), nil
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
// no conversion is needed. It allocates nothing: it walks the value and
// answers.
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

// encoder writes one document. delim is the active delimiter; num is scratch
// space for formatting floats, kept here so it is allocated once per document
// rather than once per number.
type encoder struct {
	sb    strings.Builder
	delim byte
	num   []byte
}

func (e *encoder) writeValue(v any, depth int) error {
	switch t := v.(type) {
	case map[string]any:
		e.writeMap(t, depth)
		return nil
	case []any:
		e.writeList("", t, depth, true)
		return nil
	default:
		// A scalar on its own is not a TOON document: the format's top level
		// is a mapping or a list, and a bare value has nowhere to attach.
		return fmt.Errorf("toon: a document must be an object or an array, not %T", v)
	}
}

func (e *encoder) pad(depth int) {
	for i := 0; i < depth*indentWidth; i++ {
		e.sb.WriteByte(' ')
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (e *encoder) writeMap(m map[string]any, depth int) {
	for _, k := range sortedKeys(m) {
		e.writeField(k, m[k], depth, true)
	}
}

// writeField writes one key and its value at depth. withPad is false for the
// first field of a dash item, whose indentation the "- " has already written.
func (e *encoder) writeField(key string, v any, depth int, withPad bool) {
	switch t := v.(type) {
	case []any:
		e.writeList(key, t, depth, withPad)
		return
	}
	if withPad {
		e.pad(depth)
	}
	e.sb.WriteString(key)
	if m, ok := v.(map[string]any); ok {
		e.sb.WriteString(":\n")
		e.writeMap(m, depth+1)
		return
	}
	e.sb.WriteString(": ")
	e.writeScalar(v)
	e.sb.WriteByte('\n')
}

// writeHeader writes key[n<delim>] -- the delimiter is declared only when it is
// not the default comma, which is how the spec spells it.
func (e *encoder) writeHeader(key string, n int) {
	e.sb.WriteString(key)
	e.sb.WriteByte('[')
	writeInt(&e.sb, int64(n))
	if e.delim != ',' {
		e.sb.WriteByte(e.delim)
	}
	e.sb.WriteByte(']')
}

func (e *encoder) writeList(key string, items []any, depth int, withPad bool) {
	if withPad {
		e.pad(depth)
	}
	if key == "" {
		key = "items"
	}
	if len(items) == 0 {
		e.sb.WriteString(key)
		e.sb.WriteString("[0]:\n")
		return
	}
	if fields, ok := tabular(items); ok {
		// Written straight into the builder: no per-cell strings, no per-row
		// slice or join. On a 2,000-row table that was most of the cost.
		e.writeHeader(key, len(items))
		e.sb.WriteByte('{')
		for i, f := range fields {
			if i > 0 {
				e.sb.WriteByte(e.delim)
			}
			e.sb.WriteString(f)
		}
		e.sb.WriteString("}:\n")
		for _, it := range items {
			row := it.(map[string]any)
			e.pad(depth + 1)
			for i, f := range fields {
				if i > 0 {
					e.sb.WriteByte(e.delim)
				}
				e.writeScalar(row[f])
			}
			e.sb.WriteByte('\n')
		}
		return
	}
	e.writeHeader(key, len(items))
	if allScalars(items) {
		e.sb.WriteString(": ")
		for i, it := range items {
			if i > 0 {
				e.sb.WriteByte(e.delim)
			}
			e.writeScalar(it)
		}
		e.sb.WriteByte('\n')
		return
	}
	e.sb.WriteString(":\n")
	e.writeDashItems(items, depth+1)
}

// writeDashItems writes items as "- " entries at depth, with no key header, so
// it can be used both under a "key[n]:" line and under a bare dash.
func (e *encoder) writeDashItems(items []any, depth int) {
	for _, it := range items {
		e.pad(depth)
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
			e.sb.WriteString("-\n")
			e.writeDashItems(t, depth+1)
		case map[string]any:
			if len(t) == 0 {
				e.sb.WriteString("-\n")
				continue
			}
			// Every field of the item is rendered at the item's own depth, and
			// the dash replaces the indent of the very first line. Rendering
			// the first field at depth 0 instead would put a nested object's
			// body at the wrong level: the key would sit after the dash but
			// its contents would be written as if the item were the document.
			e.sb.WriteString("- ")
			for i, k := range sortedKeys(t) {
				e.writeField(k, t[k], depth+1, i > 0)
			}
		default:
			e.sb.WriteString("- ")
			e.writeScalar(it)
			e.sb.WriteByte('\n')
		}
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
func (e *encoder) writeScalar(v any) {
	switch t := v.(type) {
	case nil:
		e.sb.WriteString("null")
	case bool:
		if t {
			e.sb.WriteString("true")
		} else {
			e.sb.WriteString("false")
		}
	case int:
		writeInt(&e.sb, int64(t))
	case int8:
		writeInt(&e.sb, int64(t))
	case int16:
		writeInt(&e.sb, int64(t))
	case int32:
		writeInt(&e.sb, int64(t))
	case int64:
		writeInt(&e.sb, t)
	case float64:
		e.writeFloat(t, 64)
	case float32:
		e.writeFloat(float64(t), 32)
	case string:
		if needsQuote(t, e.delim) {
			writeQuoted(&e.sb, t)
			return
		}
		e.sb.WriteString(t)
	default:
		e.sb.WriteString(formatScalar(v, e.delim))
	}
}

func (e *encoder) writeFloat(f float64, bits int) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// No number spells these, so they are written as the text Go gives
		// them, quoted so they cannot be mistaken for a number on the way back.
		writeQuoted(&e.sb, strconv.FormatFloat(f, 'g', -1, bits))
		return
	}
	e.num = appendCanonicalFloat(e.num[:0], f, bits)
	e.sb.Write(e.num)
}

// writeQuoted writes s as a double-quoted string with the JSON escapes.
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

func allScalars(items []any) bool {
	for _, it := range items {
		if !isScalar(it) {
			return false
		}
	}
	return true
}

func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// formatScalar renders the scalars writeScalar does not handle inline: an
// unsigned integer, a json.Number, and anything else -- which has no TOON
// spelling and is written as its quoted text so it cannot be misread.
func formatScalar(v any, delim byte) string {
	switch t := v.(type) {
	case json.Number:
		if t == "-0" {
			return "0" // the spec's canonical zero; see appendCanonicalFloat
		}
		return t.String()
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
	default:
		return quote(fmt.Sprint(t))
	}
}

// appendCanonicalFloat writes f the way the TOON spec's canonical form asks:
// plain decimal digits with no trailing zeros, no exponent between 1e-6 and
// 1e21, and -0 written as 0. Outside that range it uses an exponent, exactly as
// encoding/json does, so the digits are the same ones a JSON round trip would
// have produced -- only -0 differs, and only because the spec says so.
func appendCanonicalFloat(b []byte, f float64, bits int) []byte {
	if f == 0 {
		return append(b, '0')
	}
	return appendJSONFloat(b, f, bits)
}

// appendJSONFloat is encoding/json's float formatting, byte for byte: the
// shortest digits that read back to the same value, switching to an exponent
// below 1e-6 and from 1e21, with the exponent's leading zero removed. It is the
// reference Decode uses when it has to reproduce what encoding/json would have
// parsed, so it keeps json's "-0" rather than the canonical "0".
func appendJSONFloat(b []byte, f float64, bits int) []byte {
	format := byte('f')
	if abs := math.Abs(f); abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) ||
			bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			format = 'e'
		}
	}
	b = strconv.AppendFloat(b, f, format, -1, bits)
	if format == 'e' {
		// clean up e-09 to e-9
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
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

// needsQuote reports whether s, written bare, would read back as something
// else. The delimiter matters: a value is split on it inside a row or an
// inline array, so it must be quoted when it contains it -- and ONLY then, which
// is what makes the tab delimiter cheap for prose full of commas.
func needsQuote(s string, delim byte) bool {
	if s == "" || s == "null" || s == "true" || s == "false" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return true
	}
	if strings.ContainsAny(s, "\":\n\r\t\\[]{}") || strings.IndexByte(s, delim) >= 0 {
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
	writeQuoted(&b, s)
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
