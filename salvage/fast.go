// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"encoding"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// The fast path. One shape of reply dominates: prose around a single JSON
// object, read into a struct of ordinary fields. For that shape the general
// path walks the text to find the object, parses it to find where it ends, and
// parses it again to fill the destination — three passes over the same bytes,
// two of them inside encoding/json. This file does it in one.
//
// It is a shortcut, never a second opinion. Every doubt ends the same way:
// return false, and the caller runs exactly the code that ran before. The rule
// it keeps is that the fast path reports success ONLY when it has decoded the
// value completely and encoding/json would have decoded it to the same thing;
// a value it merely suspects it could handle is one it declines. That is why
// it refuses escapes in strings, numbers it cannot parse, values it cannot
// assign, keys whose case does not match, nested values for a field, custom
// unmarshalers, embedded structs and the ",string" option: not because they are
// wrong, but because being sure about them costs more than handing them back.
//
// useFast exists so the tests can run the same inputs through both paths and
// compare, which is the only evidence that "identical behaviour" is true.
var useFast = true

// maxFastFields caps the work a single key lookup can do, and with it the
// layout built for a type. A struct wider than this goes to encoding/json,
// which indexes its fields instead of scanning them.
const maxFastFields = 32

// maxFastDepth caps nesting the fast path will walk past. encoding/json allows
// far more; going deeper here just means the general path handles it.
const maxFastDepth = 32

// maxFastAssign caps how many members of one object the fast path will hold
// before writing them. An object with more than this many members the
// destination answers to goes to encoding/json.
const maxFastAssign = 24

// assign is one member the parse has read and the destination has not been
// given yet: where in the struct it goes, and the value, as a span of the text
// for a string and as raw bits for everything else.
type assign struct {
	num   uint64
	lo    int // offsets are plain ints: a span of a text this package was
	hi    int // handed must not silently wrap because the text was large
	index int16
	kind  reflect.Kind
}

// fastAcc collects a whole object before any of it reaches the destination.
//
// That is what lets the fast path be abandoned half way: the caller's value is
// written once, after the last byte of the object has been read, or never. The
// obvious alternative — decoding into a second copy of the destination and
// assigning that on success — says the same thing, but it pays an allocation
// and a reflect type lookup on every reply to say it, and this path exists to
// stop paying for answers it already has. Nothing in here holds a pointer, so
// it stays in the caller's frame.
type fastAcc struct {
	n   int
	buf [maxFastAssign]assign
}

func (a *fastAcc) add(v assign) bool {
	if a.n == len(a.buf) {
		return false
	}
	a.buf[a.n] = v
	a.n++
	return true
}

// fastField is one struct field the fast path may fill.
//
// A field whose type the fast path cannot set is kept in this list rather than
// left out of it, with kind reflect.Invalid. Leaving it out would make a key
// naming it look unknown, and an unknown key is skipped — where encoding/json
// would have filled the field, or refused the value and with it the whole
// candidate. Keeping it means such a key ends the fast path instead.
type fastField struct {
	name  string
	index int
	kind  reflect.Kind // String, Bool, Int64, Uint64, Float64, or Invalid
	bits  int          // for the numeric kinds, the field's width
}

type fastLayout struct{ fields []fastField }

// fastLayouts maps a struct type to its layout, or to a nil layout for the
// types the fast path does not handle. Building it walks the struct tags, so
// it is done once per type rather than once per reply.
var fastLayouts sync.Map // reflect.Type -> *fastLayout

// layoutFor is the layout for a destination, or nil for one the fast path does
// not handle. The kind is checked before the map is, because a destination
// that is not a struct -- an interface, a map, a slice -- is the common way to
// miss, and missing should not cost a lookup.
func layoutFor(dst reflect.Value) *fastLayout {
	if dst.Kind() != reflect.Struct {
		return nil
	}
	t := dst.Type()
	if v, ok := fastLayouts.Load(t); ok {
		l, _ := v.(*fastLayout)
		return l
	}
	l := buildLayout(t)
	fastLayouts.Store(t, l)
	return l
}

var (
	unmarshalerType     = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// hasCustomDecoding reports whether encoding/json would hand t to code of its
// own. Such a type is never touched here: reproducing what someone else's
// UnmarshalJSON does is not a shortcut, it is a guess.
func hasCustomDecoding(t reflect.Type) bool {
	p := reflect.PointerTo(t)
	return t.Implements(unmarshalerType) || t.Implements(textUnmarshalerType) ||
		p.Implements(unmarshalerType) || p.Implements(textUnmarshalerType)
}

func buildLayout(t reflect.Type) *fastLayout {
	if hasCustomDecoding(t) {
		return nil
	}
	if t.NumField() > maxFastFields {
		return nil
	}
	fields := make([]fastField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			// An embedded struct lifts its fields into this one under rules
			// about depth and conflicts that are encoding/json's to apply.
			return nil
		}
		if !f.IsExported() {
			continue // encoding/json never fills these, and nor does any key
		}
		name, ok := fastFieldName(f)
		if !ok {
			return nil
		}
		if name == "" {
			continue // json:"-": the field is not addressable by any key
		}
		kind, bits := fastFieldKind(f.Type)
		fields = append(fields, fastField{name: name, index: i, kind: kind, bits: bits})
	}
	for i := range fields {
		for j := i + 1; j < len(fields); j++ {
			if fields[i].name == fields[j].name {
				// Two fields answering to one name: which wins is a rule in
				// encoding/json, so let it be the one to apply it.
				return nil
			}
		}
	}
	return &fastLayout{fields: fields}
}

// fastFieldName is the JSON name of f, "" if no key can name it, and ok=false
// if working that out is encoding/json's business rather than ours.
func fastFieldName(f reflect.StructField) (name string, ok bool) {
	tag, tagged := f.Tag.Lookup("json")
	if !tagged {
		return f.Name, plainName(f.Name)
	}
	name, opts, hadComma := strings.Cut(tag, ",")
	if name == "-" && !hadComma {
		return "", true
	}
	for opts != "" {
		var o string
		o, opts, _ = strings.Cut(opts, ",")
		if o == "string" {
			// The value arrives as a JSON string holding a number; reading it
			// is a second set of rules and not worth repeating here.
			return "", false
		}
	}
	if name == "" {
		return f.Name, plainName(f.Name)
	}
	return name, plainName(name)
}

// plainName reports whether a name is ordinary enough to compare as bytes.
// encoding/json accepts tag names with punctuation in them and falls back to
// the field name for the ones it rejects; rather than repeat either rule, a
// name outside this set sends the whole type to encoding/json.
func plainName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// fastFieldKind reduces a field's type to the one kind the fast path sets it
// from, or Invalid for a field only encoding/json can fill.
func fastFieldKind(t reflect.Type) (reflect.Kind, int) {
	if hasCustomDecoding(t) {
		return reflect.Invalid, 0
	}
	switch k := t.Kind(); k {
	case reflect.String, reflect.Bool:
		return k, 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.Int64, t.Bits()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.Uint64, t.Bits()
	case reflect.Float32, reflect.Float64:
		return reflect.Float64, t.Bits()
	}
	return reflect.Invalid, 0
}

// find locates the field a key names. It returns -1 for a key no field
// answers to, which is a key to skip, and ok=false when the answer is
// encoding/json's to give: it matches a key to a field case-insensitively when
// no field matches exactly, under folding rules that are its own.
func (l *fastLayout) find(key string) (idx int, ok bool) {
	for i := range l.fields {
		if n := l.fields[i].name; len(n) == len(key) && n[0] == key[0] && n == key {
			return i, true
		}
	}
	for i := range l.fields {
		if strings.EqualFold(l.fields[i].name, key) {
			return 0, false
		}
	}
	return -1, true
}

// fastPath is Decode's whole job for the shape it is asked for most.
//
// Decode tries the whole text as one value and then each candidate in order.
// The first candidate begins at the first opening bracket in the text; when
// that bracket is a '{' — no '[' before it — and the object there is valid
// JSON, the whole-text attempt could only ever have decoded that same object,
// because a struct destination takes nothing else and anything before the
// brace would have stopped the whole text from being a value. So the object
// found here is the value Decode would have arrived at, and arriving at it
// directly skips the scan and the parse that only establishes an end the parse
// already knows.
func fastPath(text string, dst reflect.Value) bool {
	l := layoutFor(dst)
	if l == nil {
		return false
	}
	at := indexFrom(text, '{', 0)
	if at == len(text) {
		return false
	}
	if indexFrom(text, '[', 0) < at {
		return false // an array comes first: it, not this object, is candidate one
	}
	var acc fastAcc
	if _, ok := l.parse(text, at, &acc); !ok {
		return false
	}
	l.apply(text, &acc, dst)
	return true
}

// fastExact is the fast path for a value whose extent is already known: the
// object must be the whole of s. Candidates reach decoding this way.
func fastExact(s string, dst reflect.Value) bool {
	if len(s) == 0 || s[0] != '{' {
		return false
	}
	l := layoutFor(dst)
	if l == nil {
		return false
	}
	var acc fastAcc
	end, ok := l.parse(s, 0, &acc)
	if !ok || end != len(s) {
		return false
	}
	l.apply(s, &acc, dst)
	return true
}

// apply writes a whole decoded object into the destination. It replaces the
// destination rather than merging into it, as a decode into a fresh value and
// an assignment would, so a field no key mentioned reads as zero and not as
// whatever was there before.
func (l *fastLayout) apply(s string, acc *fastAcc, dst reflect.Value) {
	dst.SetZero()
	for i := 0; i < acc.n; i++ {
		a := &acc.buf[i]
		f := dst.Field(int(a.index))
		switch a.kind {
		case reflect.String:
			// The span is a slice of the reply. Keeping it would make every
			// decoded string hold the whole reply alive, which encoding/json
			// does not do and a caller holding the result would never see
			// coming.
			f.SetString(strings.Clone(s[a.lo:a.hi]))
		case reflect.Bool:
			f.SetBool(a.num != 0)
		case reflect.Int64:
			f.SetInt(int64(a.num))
		case reflect.Uint64:
			f.SetUint(a.num)
		default:
			f.SetFloat(math.Float64frombits(a.num))
		}
	}
}

// parse reads the object literal at s[at] into acc and returns the offset just
// past its closing brace.
func (l *fastLayout) parse(s string, at int, acc *fastAcc) (int, bool) {
	i := skipSpace(s, at+1)
	if i < len(s) && s[i] == '}' {
		return i + 1, true
	}
	for {
		if i >= len(s) || s[i] != '"' {
			return 0, false
		}
		key, e, plain, ok := scanString(s, i)
		if !ok || !plain {
			// An escaped key has to be unescaped before it can be compared.
			return 0, false
		}
		idx, ok := l.find(key)
		if !ok {
			return 0, false
		}
		i = skipSpace(s, e)
		if i >= len(s) || s[i] != ':' {
			return 0, false
		}
		i, ok = l.value(s, skipSpace(s, i+1), idx, acc)
		if !ok {
			return 0, false
		}
		i = skipSpace(s, i)
		if i >= len(s) {
			return 0, false
		}
		switch s[i] {
		case ',':
			i = skipSpace(s, i+1)
		case '}':
			return i + 1, true
		default:
			return 0, false
		}
	}
}

// value reads one member value at s[i] and, when idx names a field, records it
// for the destination. idx of -1 is a value no field asked for, which is still
// validated before it is skipped: encoding/json refuses the whole object when
// any part of it is malformed, so skipping without checking would accept a
// candidate it rejects.
func (l *fastLayout) value(s string, i, idx int, acc *fastAcc) (int, bool) {
	if i >= len(s) {
		return 0, false
	}
	switch c := s[i]; c {
	case '"':
		body, e, plain, ok := scanString(s, i)
		if !ok {
			return 0, false
		}
		if idx < 0 {
			return e, true
		}
		f := &l.fields[idx]
		// An escape has to be expanded, and bytes that are not valid UTF-8 are
		// replaced rune by rune; both are encoding/json's to do.
		if f.kind != reflect.String || !plain || !utf8.ValidString(body) {
			return 0, false
		}
		return e, acc.add(assign{kind: reflect.String, index: int16(f.index),
			lo: e - 1 - len(body), hi: e - 1})
	case 't', 'f':
		want, b := "true", uint64(1)
		if c == 'f' {
			want, b = "false", 0
		}
		if !strings.HasPrefix(s[i:], want) {
			return 0, false
		}
		if idx >= 0 {
			f := &l.fields[idx]
			if f.kind != reflect.Bool {
				return 0, false
			}
			if !acc.add(assign{kind: reflect.Bool, index: int16(f.index), num: b}) {
				return 0, false
			}
		}
		return i + len(want), true
	case 'n':
		if !strings.HasPrefix(s[i:], "null") {
			return 0, false
		}
		// null leaves the destination alone, and every field the object does
		// not mention is zero already.
		return i + 4, true
	case '{', '[':
		if idx >= 0 {
			return 0, false // a value with structure needs the real decoder
		}
		return skipValue(s, i, 0)
	default:
		lit, e, ok := scanNumber(s, i)
		if !ok {
			return 0, false
		}
		if idx < 0 {
			return e, true
		}
		return e, storeNumber(lit, &l.fields[idx], acc)
	}
}

// storeNumber records a JSON number literal for a numeric field. A literal the
// field cannot hold — a fraction in an integer, a value out of range — is not
// an error here but a refusal: encoding/json rejects the candidate for it, and
// letting it do so keeps one account of what fits.
func storeNumber(lit string, f *fastField, acc *fastAcc) bool {
	a := assign{kind: f.kind, index: int16(f.index)}
	switch f.kind {
	case reflect.Int64:
		n, ok := parseInt(lit, f.bits)
		if !ok {
			return false
		}
		a.num = uint64(n)
	case reflect.Uint64:
		u, ok := parseUint(lit, f.bits)
		if !ok {
			return false
		}
		a.num = u
	case reflect.Float64:
		n, err := strconv.ParseFloat(lit, f.bits)
		if err != nil {
			return false
		}
		a.num = math.Float64bits(n)
	default:
		return false
	}
	return acc.add(a)
}

// parseInt and parseUint read the integers a JSON number literal can hold
// without strconv's work of deciding a base and a sign convention that JSON
// settled already. Anything they are not certain of -- an exponent, a
// fraction, more digits than fit -- is handed back as a refusal, and the
// candidate goes to encoding/json, which refuses it too.
func parseInt(lit string, bits int) (int64, bool) {
	neg := lit[0] == '-'
	if neg {
		lit = lit[1:]
	}
	u, ok := parseUint(lit, 64)
	if !ok {
		return 0, false
	}
	if neg {
		if u > 1<<63 {
			return 0, false
		}
		n := -int64(u)
		if bits < 64 && n < -1<<(bits-1) {
			return 0, false
		}
		return n, true
	}
	if u >= 1<<63 {
		return 0, false
	}
	n := int64(u)
	if bits < 64 && n > 1<<(bits-1)-1 {
		return 0, false
	}
	return n, true
}

func parseUint(lit string, bits int) (uint64, bool) {
	if lit == "" || len(lit) > 19 {
		// 19 digits is where uint64 stops being certain; strconv can say.
		u, err := strconv.ParseUint(lit, 10, bits)
		return u, err == nil
	}
	var u uint64
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		if !isDigit(c) {
			return 0, false // a fraction or an exponent: not an integer
		}
		u = u*10 + uint64(c-'0')
	}
	if bits < 64 && u > 1<<bits-1 {
		return 0, false
	}
	return u, true
}

// skipValue validates the JSON value at s[i] and returns the offset just past
// it. It reads nothing out: it is here so that a key nobody asked for still
// has to be well formed, because encoding/json would have refused the object
// if it were not.
func skipValue(s string, i, depth int) (int, bool) {
	if depth > maxFastDepth || i >= len(s) {
		return 0, false
	}
	switch s[i] {
	case '"':
		_, e, _, ok := scanString(s, i)
		return e, ok
	case '{':
		return skipObject(s, i, depth)
	case '[':
		return skipArray(s, i, depth)
	case 't':
		return skipLiteral(s, i, "true")
	case 'f':
		return skipLiteral(s, i, "false")
	case 'n':
		return skipLiteral(s, i, "null")
	default:
		_, e, ok := scanNumber(s, i)
		return e, ok
	}
}

func skipObject(s string, i, depth int) (int, bool) {
	i = skipSpace(s, i+1)
	if i < len(s) && s[i] == '}' {
		return i + 1, true
	}
	for {
		if i >= len(s) || s[i] != '"' {
			return 0, false
		}
		_, e, _, ok := scanString(s, i)
		if !ok {
			return 0, false
		}
		i = skipSpace(s, e)
		if i >= len(s) || s[i] != ':' {
			return 0, false
		}
		e, ok = skipValue(s, skipSpace(s, i+1), depth+1)
		if !ok {
			return 0, false
		}
		i = skipSpace(s, e)
		if i >= len(s) {
			return 0, false
		}
		switch s[i] {
		case ',':
			i = skipSpace(s, i+1)
		case '}':
			return i + 1, true
		default:
			return 0, false
		}
	}
}

func skipArray(s string, i, depth int) (int, bool) {
	i = skipSpace(s, i+1)
	if i < len(s) && s[i] == ']' {
		return i + 1, true
	}
	for {
		e, ok := skipValue(s, i, depth+1)
		if !ok {
			return 0, false
		}
		i = skipSpace(s, e)
		if i >= len(s) {
			return 0, false
		}
		switch s[i] {
		case ',':
			i = skipSpace(s, i+1)
		case ']':
			return i + 1, true
		default:
			return 0, false
		}
	}
}

func skipLiteral(s string, i int, want string) (int, bool) {
	if !strings.HasPrefix(s[i:], want) {
		return 0, false
	}
	return i + len(want), true
}

// strSpecial marks the bytes a string literal cannot simply be stepped over:
// the closing quote, the escape, and the control characters JSON forbids raw.
// Everything else is content, and the inner loop below is the one place this
// package spends most of its time, so it asks one table instead of three
// comparisons per byte.
var strSpecial [256]bool

func init() {
	for c := 0; c < 0x20; c++ {
		strSpecial[c] = true
	}
	strSpecial['"'] = true
	strSpecial['\\'] = true
}

// scanString validates the string literal at s[i] and returns its raw contents
// and the offset just past the closing quote. plain says the contents hold no
// escape, so the bytes are the string; the caller still has to be sure they
// are valid UTF-8 before using them as one.
func scanString(s string, i int) (body string, end int, plain, ok bool) {
	plain = true
	for j := i + 1; ; {
		for j < len(s) && !strSpecial[s[j]] {
			j++
		}
		if j >= len(s) {
			return "", 0, false, false
		}
		switch s[j] {
		case '"':
			return s[i+1 : j], j + 1, plain, true
		case '\\':
			plain = false
			j++
			if j >= len(s) {
				return "", 0, false, false
			}
			switch s[j] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				j++
			case 'u':
				if j+5 > len(s) || !hex4(s[j+1:j+5]) {
					return "", 0, false, false
				}
				j += 5
			default:
				return "", 0, false, false
			}
		default:
			// A raw control character is not allowed in a JSON string.
			return "", 0, false, false
		}
	}
}

func hex4(s string) bool {
	for i := 0; i < 4; i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// scanNumber validates the JSON number at s[i] and returns its literal. The
// grammar is JSON's, not strconv's: strconv would take "+1", ".5", "1." and
// "Inf", all of which encoding/json refuses.
func scanNumber(s string, i int) (lit string, end int, ok bool) {
	start := i
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	default:
		return "", 0, false
	}
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || !isDigit(s[i]) {
			return "", 0, false
		}
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= len(s) || !isDigit(s[i]) {
			return "", 0, false
		}
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	}
	return s[start:i], i, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// skipSpace steps over the four bytes JSON calls whitespace.
func skipSpace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}
