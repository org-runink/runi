// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Parse reads a document into a map. Values are string, int64, float64,
// bool, time.Time (an offset date-time), [LocalDateTime], [LocalDate],
// [LocalTime], []any and map[string]any.
func Parse(data []byte) (map[string]any, error) {
	root, err := parse(data)
	if err != nil {
		return nil, err
	}
	return tableAny(root), nil
}

// Unmarshal reads a document into v, which must be a non-nil pointer. Keys
// with no matching struct field are ignored; use a [Decoder] with
// [Decoder.DisallowUnknownFields] to refuse them.
func Unmarshal(data []byte, v any) error {
	return unmarshal(data, v, false)
}

// Decode reads all of r and unmarshals it into v, as [Unmarshal] does.
func Decode(r io.Reader, v any) error {
	return NewDecoder(r).Decode(v)
}

// A Decoder reads one document from a reader.
type Decoder struct {
	r      io.Reader
	strict bool
}

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: r} }

// DisallowUnknownFields makes Decode fail on any key that the destination
// struct has no field for, as strict decoding of a configuration file
// should: a misspelt key is then an error instead of a setting silently
// left at its default. Every unknown key is reported, each as an [*Error]
// with its path and position, joined into one error. Keys decoded into a
// map or an interface are never unknown.
func (d *Decoder) DisallowUnknownFields() { d.strict = true }

// Decode reads all of the reader and unmarshals it into v.
func (d *Decoder) Decode(v any) error {
	data, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	return unmarshal(data, v, d.strict)
}

func unmarshal(data []byte, v any, strict bool) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: the destination must be a non-nil pointer, got %T", ErrInvalid, v)
	}
	root, err := parse(data)
	if err != nil {
		return err
	}
	d := &decoder{data: data, strict: strict}
	if err := d.value(value{kind: vTable, t: root}, rv.Elem()); err != nil {
		return err
	}
	return errors.Join(d.unknown...)
}

type pathSeg struct {
	key string
	idx int // -1 for a key
}

type decoder struct {
	data    []byte
	strict  bool
	path    []pathSeg
	unknown []error
}

func (d *decoder) pathString() string {
	var b strings.Builder
	for _, s := range d.path {
		if s.idx >= 0 {
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.idx))
			b.WriteByte(']')
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(keyText(s.key))
	}
	return b.String()
}

func (d *decoder) fail(v value, format string, args ...any) error {
	e := errAt(d.data, v.pos, format, args...)
	e.Path = d.pathString()
	return e
}

var (
	timeType          = reflect.TypeOf(time.Time{})
	localDateType     = reflect.TypeOf(LocalDate{})
	localTimeType     = reflect.TypeOf(LocalTime{})
	localDateTimeType = reflect.TypeOf(LocalDateTime{})
	textUnmarshaler   = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// value stores v in rv, which is addressable.
func (d *decoder) value(v value, rv reflect.Value) error {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	s, isString := v.s.(string)
	switch rv.Type() {
	case localDateType, localTimeType, localDateTimeType:
		return d.dateTime(v, rv)
	case timeType:
		if !isString {
			return d.dateTime(v, rv)
		}
	}
	if isString && reflect.PointerTo(rv.Type()).Implements(textUnmarshaler) {
		if err := rv.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
			return d.fail(v, "%v", err)
		}
		return nil
	}
	switch rv.Kind() {
	case reflect.Interface:
		x := reflect.ValueOf(toAny(v))
		if !x.Type().AssignableTo(rv.Type()) {
			return d.mismatch(v, rv)
		}
		rv.Set(x)
		return nil
	case reflect.Struct:
		if v.kind != vTable {
			return d.mismatch(v, rv)
		}
		return d.structValue(v.t, rv)
	case reflect.Map:
		if v.kind != vTable || rv.Type().Key().Kind() != reflect.String {
			return d.mismatch(v, rv)
		}
		return d.mapValue(v.t, rv)
	case reflect.Slice:
		if v.kind != vArray && v.kind != vTables {
			return d.mismatch(v, rv)
		}
		rv.Set(reflect.MakeSlice(rv.Type(), len(v.a), len(v.a)))
		return d.elements(v, rv)
	case reflect.Array:
		if v.kind != vArray && v.kind != vTables {
			return d.mismatch(v, rv)
		}
		if len(v.a) > rv.Len() {
			return d.fail(v, "%d elements do not fit in %s", len(v.a), rv.Type())
		}
		rv.SetZero()
		return d.elements(v, rv)
	}
	return d.scalar(v, rv)
}

func (d *decoder) elements(v value, rv reflect.Value) error {
	for i, e := range v.a {
		d.path = append(d.path, pathSeg{idx: i})
		err := d.value(e, rv.Index(i))
		d.path = d.path[:len(d.path)-1]
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *decoder) scalar(v value, rv reflect.Value) error {
	switch x := v.s.(type) {
	case string:
		if rv.Kind() == reflect.String {
			rv.SetString(x)
			return nil
		}
	case bool:
		if rv.Kind() == reflect.Bool {
			rv.SetBool(x)
			return nil
		}
	case int64:
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if rv.OverflowInt(x) {
				return d.fail(v, "%d overflows %s", x, rv.Type())
			}
			rv.SetInt(x)
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if x < 0 || rv.OverflowUint(uint64(x)) {
				return d.fail(v, "%d overflows %s", x, rv.Type())
			}
			rv.SetUint(uint64(x))
			return nil
		case reflect.Float32, reflect.Float64:
			if x > 1<<53 || x < -1<<53 {
				return d.fail(v, "%d cannot be held exactly by %s", x, rv.Type())
			}
			rv.SetFloat(float64(x))
			return nil
		}
	case float64:
		if rv.Kind() == reflect.Float32 || rv.Kind() == reflect.Float64 {
			if rv.OverflowFloat(x) {
				return d.fail(v, "%g overflows %s", x, rv.Type())
			}
			rv.SetFloat(x)
			return nil
		}
	}
	return d.mismatch(v, rv)
}

func (d *decoder) dateTime(v value, rv reflect.Value) error {
	var out any
	switch x := v.s.(type) {
	case time.Time:
		if rv.Type() == timeType {
			out = x
		}
	case LocalDateTime:
		switch rv.Type() {
		case localDateTimeType:
			out = x
		case timeType:
			out = x.In(time.UTC)
		}
	case LocalDate:
		switch rv.Type() {
		case localDateType:
			out = x
		case timeType:
			out = x.In(time.UTC)
		}
	case LocalTime:
		if rv.Type() == localTimeType {
			out = x
		}
	}
	if out == nil {
		return d.mismatch(v, rv)
	}
	rv.Set(reflect.ValueOf(out))
	return nil
}

func (d *decoder) mismatch(v value, rv reflect.Value) error {
	return d.fail(v, "expected %s, got %s", wants(rv.Type()), kindName(v))
}

// kindName names a TOML value's type for messages.
func kindName(v value) string {
	switch v.kind {
	case vTable:
		return "table"
	case vArray, vTables:
		return "array"
	}
	switch v.s.(type) {
	case string:
		return "string"
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case time.Time:
		return "offset date-time"
	case LocalDateTime:
		return "local date-time"
	case LocalDate:
		return "local date"
	}
	return "local time"
}

// wants names, in TOML's terms, what a Go type can hold.
func wants(t reflect.Type) string {
	switch t {
	case timeType:
		return "date-time (" + t.String() + ")"
	case localDateType:
		return "local date"
	case localTimeType:
		return "local time"
	case localDateTimeType:
		return "local date-time"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Struct:
		return "table"
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return "a map with string keys, not " + t.String()
		}
		return "table"
	case reflect.Slice, reflect.Array:
		return "array"
	}
	return "a value for Go type " + t.String()
}

func (d *decoder) mapValue(t *table, rv reflect.Value) error {
	mt := rv.Type()
	if rv.IsNil() {
		rv.Set(reflect.MakeMapWithSize(mt, len(t.keys)))
	}
	for i, k := range t.keys {
		ev := reflect.New(mt.Elem()).Elem()
		d.path = append(d.path, pathSeg{key: k, idx: -1})
		err := d.value(t.vals[i], ev)
		d.path = d.path[:len(d.path)-1]
		if err != nil {
			return err
		}
		rv.SetMapIndex(reflect.ValueOf(k).Convert(mt.Key()), ev)
	}
	return nil
}

func (d *decoder) structValue(t *table, rv reflect.Value) error {
	fields := fieldsOf(rv.Type())
	for i, k := range t.keys {
		d.path = append(d.path, pathSeg{key: k, idx: -1})
		var err error
		if idx, ok := fields[k]; ok {
			err = d.value(t.vals[i], fieldByIndex(rv, idx))
		} else if d.strict {
			d.unknown = append(d.unknown, d.fail(t.vals[i], "unknown key: %s has no field for it", structName(rv.Type())))
		}
		d.path = d.path[:len(d.path)-1]
		if err != nil {
			return err
		}
	}
	return nil
}

// structName names a struct type for a message; an anonymous struct's full
// type text would bury the key it is about.
func structName(t reflect.Type) string {
	if t.Name() == "" {
		return "the struct"
	}
	return t.String()
}

// fieldByIndex walks to a promoted field, allocating nil embedded pointers.
// fieldsOf promotes no field through an unexported embedded pointer, so
// every pointer met here can be set.
func fieldByIndex(rv reflect.Value, index []int) reflect.Value {
	for _, x := range index {
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				rv.Set(reflect.New(rv.Type().Elem()))
			}
			rv = rv.Elem()
		}
		rv = rv.Field(x)
	}
	return rv
}

var fieldCache sync.Map // reflect.Type -> map[string][]int

// fieldsOf maps each TOML key a struct accepts to its field index path.
//
// A field's key is its `toml:"name"` tag, or its Go name when it has none,
// and matching is case-sensitive. `toml:"-"` skips a field. The fields of an
// embedded struct with no tag name are promoted, with encoding/json's rule
// for collisions: the shallower field wins; at equal depth a tagged field
// beats untagged ones; otherwise the name is ambiguous and matches nothing.
func fieldsOf(t reflect.Type) map[string][]int {
	if f, ok := fieldCache.Load(t); ok {
		return f.(map[string][]int)
	}
	type cand struct {
		index  []int
		depth  int
		tagged bool
	}
	best := map[string]cand{}
	ambiguous := map[string]bool{}
	consider := func(name string, c cand) {
		b, ok := best[name]
		switch {
		case !ok || c.depth < b.depth || c.depth == b.depth && c.tagged && !b.tagged:
			best[name] = c
			ambiguous[name] = false
		case c.depth == b.depth && c.tagged == b.tagged:
			ambiguous[name] = true
		}
	}
	visiting := map[reflect.Type]bool{t: true}
	var walk func(t reflect.Type, index []int)
	walk = func(t reflect.Type, index []int) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("toml")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			idx := append(index[:len(index):len(index)], i)
			if f.Anonymous && name == "" {
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					if (f.IsExported() || f.Type.Kind() != reflect.Pointer) && !visiting[ft] {
						visiting[ft] = true
						walk(ft, idx)
						visiting[ft] = false
					}
					continue
				}
			}
			if !f.IsExported() {
				continue
			}
			tagged := name != ""
			if !tagged {
				name = f.Name
			}
			consider(name, cand{index: idx, depth: len(index), tagged: tagged})
		}
	}
	walk(t, nil)
	out := make(map[string][]int, len(best))
	for name, c := range best {
		if !ambiguous[name] {
			out[name] = c.index
		}
	}
	fieldCache.Store(t, out)
	return out
}

// toAny converts a parsed value into the types Parse documents.
func toAny(v value) any {
	switch v.kind {
	case vTable:
		return tableAny(v.t)
	case vArray, vTables:
		a := make([]any, len(v.a))
		for i, e := range v.a {
			a[i] = toAny(e)
		}
		return a
	}
	return v.s
}

func tableAny(t *table) map[string]any {
	m := make(map[string]any, len(t.keys))
	for i, k := range t.keys {
		m[k] = toAny(t.vals[i])
	}
	return m
}
