// SPDX-License-Identifier: BSD-3-Clause

package toon

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// This file moves Go values to and from the generic tree (maps, slices and
// scalars) that the writer and the parser work on, without producing JSON.
//
// The rules it applies are encoding/json's, because Encode and Decode promise
// "a type that round-trips as JSON needs no new tags". Where json's rules get
// intricate -- custom marshalers, embedded structs and field promotion, the
// ",string" option, Go arrays, non-string map keys -- these walkers do not try
// to re-implement them. They return errNeedsJSON and the caller takes the old
// route through encoding/json, so a rare shape costs speed and never
// correctness. The property tests hold both routes to identical results.

// errNeedsJSON is never returned to a caller. It means "this value has a shape
// whose rules belong to encoding/json; let it decide".
var errNeedsJSON = errors.New("toon: shape handled by encoding/json")

// maxDepth bounds the walk. encoding/json detects pointer cycles after a
// thousand levels of indirection and reports them; handing anything that deep
// to it gets its error rather than a stack overflow here.
const maxDepth = 1000

var (
	jsonMarshaler   = reflect.TypeFor[json.Marshaler]()
	jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()
	textMarshaler   = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	numberType      = reflect.TypeFor[json.Number]()
)

// customCache remembers, per type, whether it brings its own JSON or text
// (un)marshalling. Asking reflect is not free and the answer never changes.
var customCache sync.Map // reflect.Type -> bool

// custom reports whether t, or a pointer to t, defines how it is marshalled or
// unmarshalled. encoding/json calls those methods, so only it can apply them.
func custom(t reflect.Type) bool {
	// The predeclared scalar types -- int, string, float64 and the rest --
	// have no methods and never will, and they are most of the values a
	// table holds. Answering them here skips the cache lookup per cell.
	if t.Kind() <= reflect.Complex128 || t.Kind() == reflect.String {
		if t.PkgPath() == "" {
			return false
		}
	}
	if c, ok := customCache.Load(t); ok {
		return c.(bool)
	}
	p := reflect.PointerTo(t)
	c := t == numberType
	for _, i := range []reflect.Type{jsonMarshaler, jsonUnmarshaler, textMarshaler, textUnmarshaler} {
		c = c || t.Implements(i) || p.Implements(i)
	}
	customCache.Store(t, c)
	return c
}

// field is one struct field as encoding/json names it.
type field struct {
	name      string
	index     int
	omitEmpty bool
}

// structInfo is a struct type's fields in encoding/json's terms. ok is false
// when the struct uses something whose rules are left to encoding/json.
type structInfo struct {
	ok     bool
	fields []field
	byName map[string]int
}

var structCache sync.Map // reflect.Type -> *structInfo

func structOf(t reflect.Type) *structInfo {
	if si, ok := structCache.Load(t); ok {
		return si.(*structInfo)
	}
	si := buildStruct(t)
	structCache.Store(t, si)
	return si
}

func buildStruct(t reflect.Type) *structInfo {
	declined := &structInfo{}
	si := &structInfo{ok: true, byName: map[string]int{}}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Anonymous {
			return declined // embedding: json's promotion and dominance rules
		}
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if !validTag(name) {
			name = sf.Name
		}
		f := field{name: name, index: i}
		for opts != "" {
			var o string
			o, opts, _ = strings.Cut(opts, ",")
			switch o {
			case "omitempty":
				f.omitEmpty = true
			case "string", "omitzero":
				return declined
			}
		}
		if _, dup := si.byName[name]; dup {
			return declined // two fields one name: json's tie-breaking rules
		}
		si.byName[name] = len(si.fields)
		si.fields = append(si.fields, f)
	}
	return si
}

// validTag is encoding/json's rule for whether a tag's name is used or the Go
// field name is kept instead.
func validTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// emptyValue is encoding/json's definition of "empty" for omitempty.
func emptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

// validString is what encoding/json does to a string that is not valid UTF-8:
// every byte that does not start a valid rune becomes U+FFFD.
func validString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// toTree converts v to the generic tree encoding/json would have produced by
// marshalling it and parsing the result back.
//
// v is always valid: Encode reaches here only with a non-nil value that is not
// already a generic tree, and a nil interface or pointer is answered before its
// Elem is taken.
func toTree(v reflect.Value, depth int) (any, error) {
	if depth > maxDepth || custom(v.Type()) {
		return nil, errNeedsJSON
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errNeedsJSON // json refuses these; let it say so
		}
		if v.Kind() == reflect.Float32 {
			return float32(f), nil
		}
		return f, nil
	case reflect.String:
		return validString(v.String()), nil
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		return toTree(v.Elem(), depth+1)
	case reflect.Slice:
		if v.IsNil() {
			return nil, nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 && !custom(v.Type().Elem()) {
			return base64.StdEncoding.EncodeToString(v.Bytes()), nil
		}
		return listTree(v, depth)
	case reflect.Array:
		return listTree(v, depth)
	case reflect.Map:
		return mapTree(v, depth)
	case reflect.Struct:
		return structTree(v, depth)
	}
	return nil, errNeedsJSON // complex, chan, func, unsafe pointer: json refuses them
}

func listTree(v reflect.Value, depth int) (any, error) {
	out := make([]any, v.Len())
	for i := range out {
		x, err := toTree(v.Index(i), depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}

func mapTree(v reflect.Value, depth int) (any, error) {
	if v.IsNil() {
		return nil, nil
	}
	kt := v.Type().Key()
	switch kt.Kind() {
	case reflect.String:
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if custom(kt) {
			return nil, errNeedsJSON // a TextMarshaler key is named by its method
		}
	default:
		return nil, errNeedsJSON
	}
	out := make(map[string]any, v.Len())
	it := v.MapRange()
	for it.Next() {
		k := it.Key()
		var name string
		switch k.Kind() {
		case reflect.String:
			name = validString(k.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			name = strconv.FormatInt(k.Int(), 10)
		default:
			name = strconv.FormatUint(k.Uint(), 10)
		}
		if _, dup := out[name]; dup {
			// Two invalid-UTF-8 keys that repair to the same string. json
			// writes both and the parse keeps one; which one is its call.
			return nil, errNeedsJSON
		}
		x, err := toTree(it.Value(), depth+1)
		if err != nil {
			return nil, err
		}
		out[name] = x
	}
	return out, nil
}

func structTree(v reflect.Value, depth int) (any, error) {
	si := structOf(v.Type())
	if !si.ok {
		return nil, errNeedsJSON
	}
	out := make(map[string]any, len(si.fields))
	for _, f := range si.fields {
		fv := v.Field(f.index)
		if f.omitEmpty && emptyValue(fv) {
			continue
		}
		x, err := toTree(fv, depth+1)
		if err != nil {
			return nil, err
		}
		out[f.name] = x
	}
	return out, nil
}

// fromTree stores a parsed document in v the way json.Unmarshal would have
// stored the same document marshalled to JSON. It returns nil or errNeedsJSON
// and nothing else: every mismatch -- a string where a number belongs, a number
// that overflows its field -- is left to encoding/json, so the caller gets the
// exact error json would have given.
//
// It is called twice. The first call is a dry run that writes nothing and only
// answers whether the walk can fill v by itself; the second, made only when it
// can, writes. The dry run is what keeps the hand-over clean: json stops at its
// first hard error (a time that will not parse, say) and fields after it stay
// as they were, so a walk that had already written some of them -- in a map's
// random order -- would leave a different value behind than json alone.
//
// In a dry run v may be a zero Value standing in for something that does not
// exist yet (behind a nil pointer, a new map element), so it is only read.
//
// The document comes from the parser, so numbers are int64 or float64 and
// containers are map[string]any and []any.
func fromTree(doc any, v reflect.Value, dry bool) error {
	if custom(v.Type()) {
		return errNeedsJSON
	}
	if doc == nil {
		// json: null sets a pointer, interface, map or slice to nil and
		// leaves everything else alone.
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
			if !dry {
				v.SetZero()
			}
		}
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		switch {
		case !v.IsNil():
		case dry:
			return fromTree(doc, reflect.Zero(v.Type().Elem()), dry)
		default:
			v.Set(reflect.New(v.Type().Elem()))
		}
		return fromTree(doc, v.Elem(), dry)
	case reflect.Interface:
		return intoInterface(doc, v, dry)
	case reflect.Struct:
		return intoStruct(doc, v, dry)
	case reflect.Map:
		return intoMap(doc, v, dry)
	case reflect.Slice:
		return intoSlice(doc, v, dry)
	case reflect.Bool:
		b, ok := doc.(bool)
		if !ok {
			return errNeedsJSON
		}
		if !dry {
			v.SetBool(b)
		}
		return nil
	case reflect.String:
		s, ok := doc.(string)
		if !ok {
			return errNeedsJSON
		}
		if !dry {
			v.SetString(s)
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intoInt(doc, v, dry)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return intoUint(doc, v, dry)
	case reflect.Float32, reflect.Float64:
		return intoFloat(doc, v, dry)
	}
	return errNeedsJSON // Go arrays, complex, chan, func
}

// intoInterface stores the document as encoding/json would in an `any`:
// objects as map[string]any, arrays as []any, every number as float64.
func intoInterface(doc any, v reflect.Value, dry bool) error {
	if v.NumMethod() != 0 {
		return errNeedsJSON // json decodes into the value an interface holds
	}
	if !v.IsNil() {
		if e := v.Elem(); e.Kind() == reflect.Pointer && !e.IsNil() {
			return errNeedsJSON // ... and into what a held pointer points at
		}
	}
	if dry {
		if !finite(doc) {
			return errNeedsJSON // json cannot carry NaN or infinity, and says so
		}
		return nil
	}
	v.Set(reflect.ValueOf(asJSONGeneric(doc)))
	return nil
}

// finite reports whether the document holds no NaN or infinity.
func finite(doc any) bool {
	switch t := doc.(type) {
	case float64:
		return !math.IsNaN(t) && !math.IsInf(t, 0)
	case map[string]any:
		for _, x := range t {
			if !finite(x) {
				return false
			}
		}
	case []any:
		for _, x := range t {
			if !finite(x) {
				return false
			}
		}
	}
	return true
}

// asJSONGeneric turns every int64 in the document into the float64 json would
// have produced, in place. The document is the parser's own and is stored
// exactly once, after the dry run has passed, so changing it is safe.
func asJSONGeneric(doc any) any {
	switch t := doc.(type) {
	case int64:
		return float64(t)
	case map[string]any:
		for k, x := range t {
			t[k] = asJSONGeneric(x)
		}
	case []any:
		for i, x := range t {
			t[i] = asJSONGeneric(x)
		}
	}
	return doc
}

func intoStruct(doc any, v reflect.Value, dry bool) error {
	m, ok := doc.(map[string]any)
	if !ok {
		return errNeedsJSON
	}
	si := structOf(v.Type())
	if !si.ok {
		return errNeedsJSON
	}
	for k, x := range m {
		i, ok := si.byName[k]
		if !ok {
			// json also matches a key case-insensitively, and when two keys
			// fold onto one field the one later in the text wins. A parsed
			// map has no order to consult, so any such key goes to json.
			for _, f := range si.fields {
				if strings.EqualFold(f.name, k) {
					return errNeedsJSON
				}
			}
			continue // a key the struct does not have: json ignores it too
		}
		if err := fromTree(x, v.Field(si.fields[i].index), dry); err != nil {
			return err
		}
	}
	return nil
}

func intoMap(doc any, v reflect.Value, dry bool) error {
	m, ok := doc.(map[string]any)
	if !ok {
		return errNeedsJSON
	}
	t := v.Type()
	if t.Key().Kind() != reflect.String || custom(t.Key()) {
		return errNeedsJSON
	}
	if dry {
		zero := reflect.Zero(t.Elem())
		for _, x := range m {
			if err := fromTree(x, zero, dry); err != nil {
				return err
			}
		}
		return nil
	}
	if v.IsNil() {
		v.Set(reflect.MakeMapWithSize(t, len(m)))
	}
	for k, x := range m {
		// json decodes each value into a fresh zero element, not into the
		// value already in the map under that key.
		ev := reflect.New(t.Elem()).Elem()
		_ = fromTree(x, ev, dry) // the dry run has already checked it
		v.SetMapIndex(reflect.ValueOf(k).Convert(t.Key()), ev)
	}
	return nil
}

func intoSlice(doc any, v reflect.Value, dry bool) error {
	a, ok := doc.([]any)
	if !ok {
		return errNeedsJSON // including a base64 string for a []byte
	}
	if dry {
		// json decodes INTO the elements the backing array already holds, up
		// to its capacity, and past that from zero; the dry run reads the same.
		held := v.Slice(0, v.Cap())
		for i, x := range a {
			el := reflect.Zero(v.Type().Elem())
			if i < held.Len() {
				el = held.Index(i)
			}
			if err := fromTree(x, el, dry); err != nil {
				return err
			}
		}
		return nil
	}
	if len(a) == 0 {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil
	}
	if len(a) > v.Cap() {
		ns := reflect.MakeSlice(v.Type(), len(a), len(a))
		reflect.Copy(ns, v.Slice(0, v.Cap()))
		v.Set(ns)
	} else {
		v.SetLen(len(a))
	}
	for i, x := range a {
		_ = fromTree(x, v.Index(i), dry) // the dry run has already checked it
	}
	return nil
}

// jsonText is the number as encoding/json would have written it -- what its
// decoder then parses into the destination.
func jsonText(f float64) string { return string(appendJSONFloat(nil, f, 64)) }

func intoInt(doc any, v reflect.Value, dry bool) error {
	var n int64
	switch t := doc.(type) {
	case int64:
		n = t
	case float64:
		var err error
		if n, err = strconv.ParseInt(jsonText(t), 10, 64); err != nil {
			return errNeedsJSON // a fraction or an exponent: json refuses it
		}
	default:
		return errNeedsJSON
	}
	if v.OverflowInt(n) {
		return errNeedsJSON
	}
	if !dry {
		v.SetInt(n)
	}
	return nil
}

func intoUint(doc any, v reflect.Value, dry bool) error {
	var n uint64
	switch t := doc.(type) {
	case int64:
		if t < 0 {
			return errNeedsJSON
		}
		n = uint64(t)
	case float64:
		var err error
		if n, err = strconv.ParseUint(jsonText(t), 10, 64); err != nil {
			return errNeedsJSON
		}
	default:
		return errNeedsJSON
	}
	if v.OverflowUint(n) {
		return errNeedsJSON
	}
	if !dry {
		v.SetUint(n)
	}
	return nil
}

// exactInFloat64 is the range in which every int64 converts to float64 with no
// rounding at all.
const exactInFloat64 = 1 << 53

func intoFloat(doc any, v reflect.Value, dry bool) error {
	bits := v.Type().Bits()
	var f float64
	switch t := doc.(type) {
	case int64:
		if t >= -exactInFloat64 && t <= exactInFloat64 {
			// Exact in float64, so converting to the destination rounds once,
			// as parsing the decimal digits would.
			f = float64(t)
			break
		}
		// Beyond 2^53 the conversion could round twice, so the digits are
		// parsed instead, as json does. Every int64 is far inside float32's
		// range, so this parse cannot fail.
		f, _ = strconv.ParseFloat(strconv.FormatInt(t, 10), bits)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return errNeedsJSON
		}
		f = t
		if bits == 32 {
			// Rounding the float64 to float32 can differ from parsing the
			// decimal text straight to float32 (double rounding), and json
			// parses the text. So does this. A value beyond float32's range
			// fails to parse, which is json's overflow error -- so no separate
			// overflow check is needed for either width.
			var err error
			if f, err = strconv.ParseFloat(jsonText(t), 32); err != nil {
				return errNeedsJSON
			}
		}
	default:
		return errNeedsJSON
	}
	if !dry {
		v.SetFloat(f)
	}
	return nil
}
