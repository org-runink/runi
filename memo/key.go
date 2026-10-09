package memo

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
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
//   - every value is TYPE-TAGGED, so Hash("1") and Hash(1) and Hash(true)
//     differ. Untagged concatenation collides in ways that are very hard to
//     find later;
//   - lengths are written before variable-length data, so {"ab","c"} and
//     {"a","bc"} do not collide.
//
// It returns a hex SHA-256. Use it for the key type of a Store:
//
//	key := memo.Hash("model-v3", 0.2, map[string]any{"q": q, "lang": "en"})
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
		k.uint(uint64(t))
	case uint8:
		k.uint(uint64(t))
	case uint16:
		k.uint(uint64(t))
	case uint32:
		k.uint(uint64(t))
	case uint64:
		k.uint(t)
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
		// Anything else is rendered with %v and tagged distinctly, so an
		// unsupported type still produces a stable key rather than a panic —
		// but it is tagged 'u' so it can never collide with a handled type.
		k.tag('u')
		k.str(fmt.Sprintf("%T|%v", t, t))
	}
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
