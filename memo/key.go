package memo

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"encoding/hex"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Hash builds a deterministic cache key from structured parts.
//
// The hard part of an exact-match cache is not storage, it is deciding that two
// requests are the same request. Hash makes that decision explicit and
// repeatable:
//
//   - map keys are SORTED before hashing, because Go randomises map iteration
//     order and hashing a map directly would produce a different key every run
//     for identical input — a cache that never hits and never tells you why;
//   - values are TYPE-TAGGED, so Hash("1") and Hash(1) and Hash(true) differ.
//     Untagged concatenation collides in ways that are very hard to find later;
//   - lengths are written before variable-length data, so {"ab","c"} and
//     {"a","bc"} do not collide.
//
// It returns a hex SHA-256. Use it for the key type of a Store:
//
//	key := memo.Hash("model-v3", 0.2, map[string]any{"q": q, "lang": "en"})
//
// # Supported types
//
// Hash encodes these types directly: nil, string, []byte, bool, every int and
// uint type, float32, float64, []string, []any, map[string]any and
// map[string]string, nested to any depth through the last four. Signed
// integers of every width hash alike, as do unsigned integers of every width
// and float32 with the float64 it converts to. Every NaN hashes alike, and -0
// hashes as 0.
//
// For these types the output is stable: the same parts give the same key in
// every process, on every platform, and in every v1 release of this module,
// so a key may be persisted or shared between processes.
//
// # Other types
//
// Any other type is walked by reflection and hashed by its contents, never by
// its address: a pointer by what it points to (a nil pointer differs from a
// pointer to the zero value), a struct by all of its fields, exported or not,
// slices, arrays and maps (keys of any type, sorted) element by element, and an
// interface by the value it holds, tagged as if that value had been passed to
// Hash directly. A type that implements encoding.TextMarshaler is hashed by its
// text instead, so a time.Time is hashed as the instant it names and not its
// location pointer or monotonic reading. The type's name and package path are
// part of the key, so equal contents of two types differ, and a cyclic value is
// hashed as a cycle rather than recursing forever. Channels, funcs and unsafe
// pointers have no contents and are hashed by identity: such a key matches
// only the same channel or func, and funcs are identified by their code, so
// two closures of one function literal share a key. Leave them out of keys.
//
// Keys of these types are deterministic across runs of one build, but they are
// not covered by the stability promise above: renaming or moving a type, or
// changing its fields, changes its keys, and so may a later release of this
// module. Do not persist them beyond the life of a deployment.
//
// A value of a supported type cycling through itself, such as a []any that
// contains itself, is not detected and overflows the stack.
func Hash(parts ...any) string {
	k := keyPool.Get().(*keyBuf)
	k.b = k.b[:0]
	for _, p := range parts {
		k.value(p)
	}
	sum := sha256.Sum256(k.b)
	// A key built from one oversized argument should not keep that capacity
	// parked in the pool for the life of the process.
	if cap(k.b) <= maxPooledKey && cap(k.keys) <= maxPooledKeys {
		k.keys = k.keys[:0]
		k.seen = k.seen[:0]
		keyPool.Put(k)
	}
	return hex.EncodeToString(sum[:])
}

// keyBuf is the canonical byte form of a key, built once and hashed once.
//
// The obvious implementation writes each piece straight into the sha256 state
// through an io.Writer, and that is what this used to do. It cost 36
// allocations to hash a four-field map, because every one of those writes
// escapes: a tag is `h.Write([]byte{c})`, a length prefix is `h.Write(n[:])`
// on a local array, and every string is `[]byte(s)` copied for the call. None
// of them survives the call, but the compiler cannot know that through an
// interface. Appending into one buffer and hashing it at the end produces the
// SAME bytes and the same digest, with the allocations gone.
type keyBuf struct {
	b []byte
	// keys is scratch for sorting map keys, used as a STACK so a nested map
	// cannot clobber the one that contains it: each call appends its keys,
	// sorts only its own region, and truncates back on the way out.
	keys []string
	// seen is the pointers, maps and slices being walked, outermost first,
	// so a cyclic value is written as a reference instead of forever.
	seen []visit
}

const (
	maxPooledKey  = 64 << 10
	maxPooledKeys = 1024
)

var keyPool = sync.Pool{New: func() any {
	return &keyBuf{b: make([]byte, 0, 256), keys: make([]string, 0, 16)}
}}

func (k *keyBuf) tag(c byte)     { k.b = append(k.b, c) }
func (k *keyBuf) u64(v uint64)   { k.b = binary.BigEndian.AppendUint64(k.b, v) }
func (k *keyBuf) bytes(b []byte) { k.u64(uint64(len(b))); k.b = append(k.b, b...) }
func (k *keyBuf) str(s string)   { k.u64(uint64(len(s))); k.b = append(k.b, s...) }

func (k *keyBuf) int(v int64)   { k.tag('i'); k.u64(uint64(v)) }
func (k *keyBuf) uint(v uint64) { k.u64(v) }

// unsigned writes an unsigned integer part, tagged so it cannot be mistaken
// for any other part. uint is the untagged form, for lengths inside a part.
func (k *keyBuf) unsigned(v uint64) { k.tag('u'); k.u64(v) }

// float canonicalises NaN so that two NaNs hash alike, which they would not if
// their bit patterns differed, and normalises negative zero to zero so that
// -0.0 and 0.0 share a key.
func (k *keyBuf) float(v float64) {
	k.tag('f')
	if math.IsNaN(v) {
		k.u64(0x7FF8000000000001)
		return
	}
	if v == 0 {
		v = 0
	}
	k.u64(math.Float64bits(v))
}

func (k *keyBuf) value(v any) {
	switch t := v.(type) {
	case nil:
		k.tag('z')
	case string:
		k.tag('s')
		k.str(t)
	case []byte:
		k.tag('b')
		k.bytes(t)
	case bool:
		k.tag('o')
		if t {
			k.bytes([]byte{1})
		} else {
			k.bytes([]byte{0})
		}
	case int:
		k.int(int64(t))
	case int8:
		k.int(int64(t))
	case int16:
		k.int(int64(t))
	case int32:
		k.int(int64(t))
	case int64:
		k.int(t)
	case uint:
		k.unsigned(uint64(t))
	case uint8:
		k.unsigned(uint64(t))
	case uint16:
		k.unsigned(uint64(t))
	case uint32:
		k.unsigned(uint64(t))
	case uint64:
		k.unsigned(t)
	case float32:
		k.float(float64(t))
	case float64:
		k.float(t)
	case []string:
		k.tag('l')
		k.uint(uint64(len(t)))
		for _, s := range t {
			k.value(s)
		}
	case []any:
		k.tag('l')
		k.uint(uint64(len(t)))
		for _, e := range t {
			k.value(e)
		}
	case map[string]any:
		k.tag('m')
		at := pushKeys(k, t)
		k.uint(uint64(len(k.keys) - at))
		for _, key := range k.keys[at:] {
			k.value(key)
			k.value(t[key])
		}
		k.keys = k.keys[:at]
	case map[string]string:
		k.tag('m')
		at := pushKeys(k, t)
		k.uint(uint64(len(k.keys) - at))
		for _, key := range k.keys[at:] {
			k.value(key)
			k.value(t[key])
		}
		k.keys = k.keys[:at]
	default:
		// Every other type is walked by reflection: its contents, never its
		// address. This used to render the value with %v, which printed a
		// pointer -- and so any struct holding one -- as an address, and a
		// key that differs on every request is a cache that never hits.
		k.reflected(reflect.ValueOf(t))
	}
}

// reflected writes a value of a type the switch in value does not list: the
// type's identity, so equal contents of different types do not collide, then
// the contents. Tag 'v' is used nowhere else.
func (k *keyBuf) reflected(v reflect.Value) {
	k.tag('v')
	k.str(typeID(v.Type()))
	k.walk(v)
}

var textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()

// walk writes v's contents. The static type is already known from the
// enclosing type, so only what the type does not fix is written: lengths,
// nil-ness, and the dynamic type behind an interface.
func (k *keyBuf) walk(v reflect.Value) {
	t := v.Type()
	if t.Implements(textMarshaler) {
		// The type's own canonical text, when it has one, rather than its
		// internals: a time.Time's location pointer and monotonic reading are
		// not part of which instant it is. The marker byte is written for
		// every value of such a type, so the two forms cannot be confused.
		if (v.Kind() != reflect.Pointer && v.Kind() != reflect.Interface || !v.IsNil()) && v.CanInterface() {
			if text, err := v.Interface().(encoding.TextMarshaler).MarshalText(); err == nil {
				k.tag(1)
				k.bytes(text)
				return
			}
		}
		k.tag(0)
	}
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			k.tag(1)
		} else {
			k.tag(0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		k.u64(uint64(v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		k.u64(v.Uint())
	case reflect.Float32, reflect.Float64:
		k.float(v.Float())
	case reflect.Complex64, reflect.Complex128:
		c := v.Complex()
		k.float(real(c))
		k.float(imag(c))
	case reflect.String:
		k.str(v.String())
	case reflect.Array:
		for i := range v.Len() {
			k.walk(v.Index(i))
		}
	case reflect.Struct:
		// Unexported fields too: two values that differ only there are two
		// different requests, and a cache that confused them would answer one
		// with the other's result.
		for i := range v.NumField() {
			k.walk(v.Field(i))
		}
	case reflect.Interface:
		switch e := v.Elem(); {
		case !e.IsValid():
			k.tag('z')
		case e.CanInterface():
			k.value(e.Interface()) // tagged exactly as the same value passed to Hash
		default:
			k.reflected(e) // behind an unexported field: Interface would panic
		}
	case reflect.Pointer:
		if k.enter(v, 0) {
			k.walk(v.Elem())
			k.leave()
		}
	case reflect.Slice:
		if k.enter(v, v.Len()) {
			k.u64(uint64(v.Len()))
			if t.Elem().Kind() == reflect.Uint8 {
				k.b = append(k.b, v.Bytes()...)
			} else {
				for i := range v.Len() {
					k.walk(v.Index(i))
				}
			}
			k.leave()
		}
	case reflect.Map:
		if k.enter(v, 0) {
			k.mapEntries(v)
			k.leave()
		}
	default:
		// Chan, Func and UnsafePointer have no contents to hash, only an
		// identity. This is the old %v behaviour for them, and as before such
		// a key only ever matches the same channel or function.
		k.u64(uint64(v.Pointer()))
	}
}

// mapEntries writes a map's entries sorted by their encoded bytes. Keys of any
// type can be ordered that way, and since a key's encoding delimits itself,
// sorting whole entries sorts by key first.
func (k *keyBuf) mapEntries(v reflect.Value) {
	k.u64(uint64(v.Len()))
	start := len(k.b)
	var ends []int
	for it := v.MapRange(); it.Next(); {
		k.walk(it.Key())
		k.walk(it.Value())
		ends = append(ends, len(k.b)-start)
	}
	region := append([]byte(nil), k.b[start:]...)
	entries := make([][]byte, len(ends))
	from := 0
	for i, end := range ends {
		entries[i] = region[from:end]
		from = end
	}
	slices.SortFunc(entries, bytes.Compare)
	k.b = k.b[:start]
	for _, e := range entries {
		k.b = append(k.b, e...)
	}
}

// visit identifies a pointer, map or slice being walked. The type is part of
// it because a struct and its first field share an address, and the length
// because two slices of one array may differ only in it.
type visit struct {
	p uintptr
	n int
	t reflect.Type
}

// enter writes the marker for a pointer, map or slice: 0 for nil, 'c' and a
// distance for one already being walked further up -- a cycle, which would
// otherwise recurse until the stack ran out -- or 1, and reports true, when
// the caller should write the contents and then call leave.
func (k *keyBuf) enter(v reflect.Value, n int) bool {
	if v.IsNil() {
		k.tag(0)
		return false
	}
	at := visit{v.Pointer(), n, v.Type()}
	for i := len(k.seen) - 1; i >= 0; i-- {
		if k.seen[i] == at {
			k.tag('c')
			k.u64(uint64(len(k.seen) - 1 - i))
			return false
		}
	}
	k.seen = append(k.seen, at)
	k.tag(1)
	return true
}

func (k *keyBuf) leave() { k.seen = k.seen[:len(k.seen)-1] }

// typeID names a type uniquely by package path rather than package name, which
// reflect.Type.String uses and two packages can share.
func typeID(t reflect.Type) string {
	if t.Name() != "" {
		if t.PkgPath() == "" {
			return t.Name() // predeclared
		}
		return t.PkgPath() + "." + t.Name()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + typeID(t.Elem())
	case reflect.Slice:
		return "[]" + typeID(t.Elem())
	case reflect.Array:
		return "[" + strconv.Itoa(t.Len()) + "]" + typeID(t.Elem())
	case reflect.Map:
		return "map[" + typeID(t.Key()) + "]" + typeID(t.Elem())
	case reflect.Struct:
		var b strings.Builder
		b.WriteString("struct{")
		for i := range t.NumField() {
			f := t.Field(i)
			b.WriteString(f.PkgPath + "." + f.Name + " " + typeID(f.Type) + ";")
		}
		b.WriteString("}")
		return b.String()
	}
	return t.String() // chan, func, interface: only their identity is hashed
}

// pushKeys appends m's keys to k.keys, sorts just that region, and returns
// where the region starts. Hashing a map otherwise allocates a fresh slice
// every call, and a map is the common case for a structured cache key.
func pushKeys[V any](k *keyBuf, m map[string]V) int {
	at := len(k.keys)
	for key := range m {
		k.keys = append(k.keys, key)
	}
	sort.Strings(k.keys[at:])
	return at
}

// NormalizeSpace collapses every run of whitespace to a single space and trims
// the ends.
//
// Use it on free text before hashing when "the same request" should survive
// reformatting — a prompt that gained a trailing newline, or was re-wrapped — and
// do NOT use it where whitespace is significant, such as code or
// whitespace-sensitive markup. It is offered separately rather than applied
// inside Hash for exactly that reason: only the caller knows which case this is.
func NormalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
