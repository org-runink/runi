package memo

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
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
	h := sha256.New()
	for _, p := range parts {
		writeValue(h, p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeValue(h io.Writer, v any) {
	switch t := v.(type) {
	case nil:
		tag(h, 'z')
	case string:
		tag(h, 's')
		writeBytes(h, []byte(t))
	case []byte:
		tag(h, 'b')
		writeBytes(h, t)
	case bool:
		tag(h, 'o')
		if t {
			writeBytes(h, []byte{1})
		} else {
			writeBytes(h, []byte{0})
		}
	case int:
		writeInt(h, int64(t))
	case int8:
		writeInt(h, int64(t))
	case int16:
		writeInt(h, int64(t))
	case int32:
		writeInt(h, int64(t))
	case int64:
		writeInt(h, t)
	case uint:
		writeUint(h, uint64(t))
	case uint8:
		writeUint(h, uint64(t))
	case uint16:
		writeUint(h, uint64(t))
	case uint32:
		writeUint(h, uint64(t))
	case uint64:
		writeUint(h, t)
	case float32:
		writeFloat(h, float64(t))
	case float64:
		writeFloat(h, t)
	case []string:
		tag(h, 'l')
		writeUint(h, uint64(len(t)))
		for _, s := range t {
			writeValue(h, s)
		}
	case []any:
		tag(h, 'l')
		writeUint(h, uint64(len(t)))
		for _, e := range t {
			writeValue(h, e)
		}
	case map[string]any:
		tag(h, 'm')
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		writeUint(h, uint64(len(keys)))
		for _, k := range keys {
			writeValue(h, k)
			writeValue(h, t[k])
		}
	case map[string]string:
		tag(h, 'm')
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		writeUint(h, uint64(len(keys)))
		for _, k := range keys {
			writeValue(h, k)
			writeValue(h, t[k])
		}
	default:
		// Anything else is rendered with %v and tagged distinctly, so an
		// unsupported type still produces a stable key rather than a panic —
		// but it is tagged 'u' so it can never collide with a handled type.
		tag(h, 'u')
		writeBytes(h, []byte(fmt.Sprintf("%T|%v", t, t)))
	}
}

func tag(h io.Writer, c byte) { _, _ = h.Write([]byte{c}) }

func writeBytes(h io.Writer, b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	_, _ = h.Write(n[:])
	_, _ = h.Write(b)
}

func writeInt(h io.Writer, v int64) {
	tag(h, 'i')
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(v))
	_, _ = h.Write(n[:])
}

func writeUint(h io.Writer, v uint64) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], v)
	_, _ = h.Write(n[:])
}

// writeFloat canonicalises NaN so that two NaNs hash alike, which they would
// not if their bit patterns differed, and normalises negative zero to zero so
// that -0.0 and 0.0 share a key.
func writeFloat(h io.Writer, v float64) {
	tag(h, 'f')
	if math.IsNaN(v) {
		v = math.NaN()
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], 0x7FF8000000000001)
		_, _ = h.Write(n[:])
		return
	}
	if v == 0 {
		v = 0
	}
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], math.Float64bits(v))
	_, _ = h.Write(n[:])
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
