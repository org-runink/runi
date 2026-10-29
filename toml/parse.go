// SPDX-License-Identifier: BSD-3-Clause

package toml

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// tkind records how a table came to exist, which is what decides whether a
// later header or dotted key may define it or add to it.
type tkind uint8

const (
	tImplicit tkind = iota // created as the parent of a [header]; a later [header] may define it
	tHeader                // defined by its own [header] or [[header]]
	tDotted                // created by a dotted key; more dotted keys may add to it
	tInline                // an inline table, or a table inside one: closed once written
)

// vkind is the shape of a parsed value.
type vkind uint8

const (
	vScalar vkind = iota
	vTable
	vArray  // a static array, written [ ... ]
	vTables // an array of tables, written [[ ... ]]
)

// value is one parsed value. pos is the byte offset of the key that defined
// it, or of the value itself for an array element.
type value struct {
	pos  int
	kind vkind
	s    any // vScalar: string, int64, float64, bool, time.Time or a Local type
	t    *table
	a    []value
}

type table struct {
	pos   int
	kind  tkind
	keys  []string
	vals  []value
	index map[string]int
}

// indexAt is the size at which a table starts keeping a map from key to
// position; below it a scan is cheaper than the map's allocations.
const indexAt = 12

func (t *table) lookup(k string) (int, bool) {
	if t.index != nil {
		i, ok := t.index[k]
		return i, ok
	}
	for i, x := range t.keys {
		if x == k {
			return i, true
		}
	}
	return 0, false
}

func (t *table) add(k string, v value) {
	t.keys = append(t.keys, k)
	t.vals = append(t.vals, v)
	switch n := len(t.keys); {
	case n == indexAt:
		t.index = make(map[string]int, 2*indexAt)
		for i, x := range t.keys {
			t.index[x] = i
		}
	case n > indexAt:
		t.index[k] = n - 1
	}
}

type parser struct {
	data   []byte
	i      int
	root   *table
	cur    *table
	intern map[string]string // bare keys, which repeat across a document
}

// bareKey returns the key at data[start:p.i], sharing one string per
// distinct key across the document.
func (p *parser) bareKey(start int) string {
	b := p.data[start:p.i]
	if k, ok := p.intern[string(b)]; ok {
		return k
	}
	k := string(b)
	if p.intern == nil {
		p.intern = make(map[string]string)
	}
	p.intern[k] = k
	return k
}

// parse reads a whole document into a table tree.
func parse(data []byte) (*table, error) {
	if off := invalidUTF8(data); off >= 0 {
		return nil, errAt(data, off, "the document is not valid UTF-8 (byte 0x%02x)", data[off])
	}
	p := &parser{data: data, root: &table{kind: tHeader}}
	p.cur = p.root
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		p.i = 3
	}
	for {
		p.skipWS()
		if p.eof() {
			return p.root, nil
		}
		var err error
		switch c := p.data[p.i]; {
		case c == '#' || c == '\n' || c == '\r':
		case c == '[':
			err = p.header()
		default:
			err = p.keyval(p.cur)
		}
		if err == nil {
			err = p.endLine()
		}
		if err != nil {
			return nil, err
		}
	}
}

// invalidUTF8 returns the offset of the first byte that is not valid UTF-8,
// or -1.
func invalidUTF8(data []byte) int {
	if utf8.Valid(data) {
		return -1
	}
	for i := 0; ; {
		r, n := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && n == 1 {
			return i
		}
		i += n
	}
}

func (p *parser) eof() bool { return p.i >= len(p.data) }

func (p *parser) err(off int, format string, args ...any) error {
	return errAt(p.data, off, format, args...)
}

// describe names what sits at off, for "expected X, found Y" messages.
func (p *parser) describe(off int) string {
	if off >= len(p.data) {
		return "the end of the document"
	}
	if c := p.data[off]; c == '\n' || c == '\r' {
		return "the end of the line"
	}
	r, _ := utf8.DecodeRune(p.data[off:])
	return strconv.QuoteRune(r)
}

func (p *parser) skipWS() {
	for !p.eof() && (p.data[p.i] == ' ' || p.data[p.i] == '\t') {
		p.i++
	}
}

// endLine consumes optional whitespace, an optional comment and the newline
// that must end every expression.
func (p *parser) endLine() error {
	p.skipWS()
	if p.eof() {
		return nil
	}
	if p.data[p.i] == '#' {
		if err := p.comment(); err != nil {
			return err
		}
		if p.eof() {
			return nil
		}
	}
	if c := p.data[p.i]; c != '\n' && c != '\r' {
		return p.err(p.i, "expected the end of the line, found %s", p.describe(p.i))
	}
	return p.newline()
}

// newline consumes LF or CRLF; a lone CR is an error.
func (p *parser) newline() error {
	if p.data[p.i] == '\r' {
		if p.i+1 >= len(p.data) || p.data[p.i+1] != '\n' {
			return p.err(p.i, "a carriage return must be followed by a line feed")
		}
		p.i++
	}
	p.i++
	return nil
}

func isControl(c byte) bool { return (c < 0x20 && c != '\t') || c == 0x7f }

// comment consumes a comment up to, not including, the end of its line.
func (p *parser) comment() error {
	for p.i++; !p.eof(); p.i++ {
		c := p.data[p.i]
		if c == '\n' || c == '\r' {
			return nil
		}
		if isControl(c) {
			return p.err(p.i, "control character U+%04X is not allowed in a comment", c)
		}
	}
	return nil
}

// skipBlank skips whitespace, newlines and comments, as allowed inside an array.
func (p *parser) skipBlank() error {
	for {
		p.skipWS()
		if p.eof() {
			return nil
		}
		var err error
		switch p.data[p.i] {
		case '#':
			err = p.comment()
		case '\n', '\r':
			err = p.newline()
		default:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func isBare(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// key reads a possibly dotted key and the offset of each part, appending
// them to parts and offs, then skips the whitespace after it.
func (p *parser) key(parts []string, offs []int) ([]string, []int, error) {
	for {
		p.skipWS()
		start := p.i
		var k string
		var err error
		switch {
		case p.eof():
			return nil, nil, p.err(p.i, "expected a key, found the end of the document")
		case p.data[p.i] == '"':
			if bytes.HasPrefix(p.data[p.i:], []byte(`"""`)) {
				return nil, nil, p.err(p.i, "a key cannot be a multi-line string")
			}
			k, err = p.basicString()
		case p.data[p.i] == '\'':
			if bytes.HasPrefix(p.data[p.i:], []byte(`'''`)) {
				return nil, nil, p.err(p.i, "a key cannot be a multi-line string")
			}
			k, err = p.literalString()
		default:
			for !p.eof() && isBare(p.data[p.i]) {
				p.i++
			}
			if p.i == start {
				return nil, nil, p.err(p.i, "expected a key, found %s", p.describe(p.i))
			}
			k = p.bareKey(start)
		}
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, k)
		offs = append(offs, start)
		p.skipWS()
		if p.eof() || p.data[p.i] != '.' {
			return parts, offs, nil
		}
		p.i++
	}
}

// keyval reads key = value and stores it in t.
func (p *parser) keyval(t *table) error {
	var pbuf [4]string
	var obuf [4]int
	parts, offs, err := p.key(pbuf[:0], obuf[:0])
	if err != nil {
		return err
	}
	if p.eof() || p.data[p.i] != '=' {
		return p.err(p.i, "expected '=' after the key %s, found %s", fmtKey(parts), p.describe(p.i))
	}
	p.i++
	p.skipWS()
	v, err := p.value()
	if err != nil {
		return err
	}
	last := len(parts) - 1
	for j, k := range parts[:last] {
		idx, ok := t.lookup(k)
		if !ok {
			nt := &table{pos: offs[j], kind: tDotted}
			t.add(k, value{pos: offs[j], kind: vTable, t: nt})
			t = nt
			continue
		}
		ev := t.vals[idx]
		if ev.kind != vTable || ev.t.kind != tDotted {
			return p.err(offs[j], "cannot add keys to %s with a dotted key: it was already defined as %s at %s",
				fmtKey(parts[:j+1]), shape(ev), p.where(ev.pos))
		}
		t = ev.t
	}
	if idx, ok := t.lookup(parts[last]); ok {
		return p.err(offs[last], "key %s is already defined at %s", fmtKey(parts), p.where(t.vals[idx].pos))
	}
	v.pos = offs[last]
	if v.kind == vTable {
		v.t.pos = offs[last]
	}
	t.add(parts[last], v)
	return nil
}

// where formats an offset as "line L:C", for messages that point elsewhere.
func (p *parser) where(off int) string {
	l, c := position(p.data, off)
	return fmt.Sprintf("line %d:%d", l, c)
}

// shape names an existing value for a redefinition message.
func shape(v value) string {
	switch {
	case v.kind == vTables:
		return "an array of tables"
	case v.kind == vArray:
		return "an array"
	case v.kind == vScalar:
		return "a value"
	case v.t.kind == tInline:
		return "an inline table"
	case v.t.kind == tDotted:
		return "a table by dotted keys"
	}
	return "a table"
}

// header reads [table] or [[array.of.tables]] and makes it current.
func (p *parser) header() error {
	start := p.i
	p.i++
	aot := !p.eof() && p.data[p.i] == '['
	if aot {
		p.i++
	}
	var pbuf [4]string
	var obuf [4]int
	parts, offs, err := p.key(pbuf[:0], obuf[:0])
	if err != nil {
		return err
	}
	closer := "]"
	if aot {
		closer = "]]"
	}
	if !bytes.HasPrefix(p.data[p.i:], []byte(closer)) {
		return p.err(p.i, "expected %q to close the table header, found %s", closer, p.describe(p.i))
	}
	p.i += len(closer)

	t := p.root
	last := len(parts) - 1
	for j, k := range parts[:last] {
		idx, ok := t.lookup(k)
		if !ok {
			nt := &table{pos: offs[j], kind: tImplicit}
			t.add(k, value{pos: offs[j], kind: vTable, t: nt})
			t = nt
			continue
		}
		ev := t.vals[idx]
		switch {
		case ev.kind == vTables:
			t = ev.a[len(ev.a)-1].t
		case ev.kind == vTable && ev.t.kind != tInline:
			t = ev.t
		default:
			return p.err(offs[j], "cannot define table %s: %s is already %s at %s",
				fmtKey(parts), fmtKey(parts[:j+1]), shape(ev), p.where(ev.pos))
		}
	}
	k := parts[last]
	idx, exists := t.lookup(k)
	if aot {
		nt := &table{pos: offs[last], kind: tHeader}
		elem := value{pos: start, kind: vTable, t: nt}
		switch {
		case !exists:
			t.add(k, value{pos: offs[last], kind: vTables, a: []value{elem}})
		case t.vals[idx].kind == vTables:
			t.vals[idx].a = append(t.vals[idx].a, elem)
		default:
			return p.err(offs[last], "cannot append to %s with [[ ]]: it is already %s at %s",
				fmtKey(parts), shape(t.vals[idx]), p.where(t.vals[idx].pos))
		}
		p.cur = nt
		return nil
	}
	if !exists {
		nt := &table{pos: offs[last], kind: tHeader}
		t.add(k, value{pos: offs[last], kind: vTable, t: nt})
		p.cur = nt
		return nil
	}
	ev := &t.vals[idx]
	if ev.kind != vTable || ev.t.kind != tImplicit {
		return p.err(offs[last], "table %s is already defined as %s at %s", fmtKey(parts), shape(*ev), p.where(ev.pos))
	}
	ev.t.kind = tHeader
	ev.pos, ev.t.pos = offs[last], offs[last]
	p.cur = ev.t
	return nil
}

// value reads one value at the cursor.
func (p *parser) value() (value, error) {
	start := p.i
	v := value{pos: start}
	var err error
	if p.eof() {
		return v, p.err(p.i, "expected a value, found the end of the document")
	}
	switch p.data[p.i] {
	case '"':
		var s string
		if bytes.HasPrefix(p.data[p.i:], []byte(`"""`)) {
			s, err = p.multiBasic()
		} else {
			s, err = p.basicString()
		}
		v.s = s
	case '\'':
		var s string
		if bytes.HasPrefix(p.data[p.i:], []byte(`'''`)) {
			s, err = p.multiLiteral()
		} else {
			s, err = p.literalString()
		}
		v.s = s
	case '[':
		return p.array()
	case '{':
		return p.inlineTable()
	default:
		v.s, err = p.scalar()
	}
	return v, err
}

func isTokenChar(c byte) bool {
	return isBare(c) || c == '+' || c == '.' || c == ':'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// scalar reads an unquoted value: a boolean, number or date-time.
func (p *parser) scalar() (any, error) {
	start := p.i
	for !p.eof() && isTokenChar(p.data[p.i]) {
		p.i++
	}
	// A date and a time may be separated by a single space.
	if p.i-start == 10 && p.i+1 < len(p.data) && p.data[p.i] == ' ' && isDigit(p.data[p.i+1]) && looksDateTime(string(p.data[start:p.i])) {
		for p.i++; !p.eof() && isTokenChar(p.data[p.i]); p.i++ {
		}
	}
	b := p.data[start:p.i]
	if n, ok := plainInt(b); ok {
		return n, nil
	}
	switch string(b) {
	case "":
		return nil, p.err(start, "expected a value, found %s", p.describe(start))
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "inf", "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	case "nan", "+nan":
		return math.NaN(), nil
	case "-nan":
		return math.Copysign(math.NaN(), -1), nil
	}
	tok := string(b)
	if looksDateTime(tok) {
		return p.dateTime(tok, start)
	}
	return p.number(tok, start)
}

// plainInt reads the common integer, an optional minus sign and at most 18
// digits with no leading zero, without the allocation of a string. Anything
// else, including every malformed integer, goes the general way.
func plainInt(b []byte) (int64, bool) {
	neg := len(b) > 0 && b[0] == '-'
	if neg {
		b = b[1:]
	}
	if len(b) == 0 || len(b) > 18 || (b[0] == '0' && len(b) > 1) {
		return 0, false
	}
	var n int64
	for _, c := range b {
		if !isDigit(c) {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

func looksDateTime(tok string) bool {
	if len(tok) >= 5 && isDigit(tok[0]) && isDigit(tok[1]) && isDigit(tok[2]) && isDigit(tok[3]) && tok[4] == '-' {
		return true
	}
	return len(tok) >= 3 && isDigit(tok[0]) && isDigit(tok[1]) && tok[2] == ':'
}

func isHex(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isOct(c byte) bool { return c >= '0' && c <= '7' }
func isBin(c byte) bool { return c == '0' || c == '1' }

// digitRun reports whether s is one or more digits with single underscores
// only between digits.
func digitRun(s string, ok func(byte) bool) bool {
	if s == "" || !ok(s[0]) || !ok(s[len(s)-1]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '_' {
			if !ok(s[i+1]) || !ok(s[i-1]) {
				return false
			}
		} else if !ok(s[i]) {
			return false
		}
	}
	return true
}

// number reads an integer or a float from an unquoted token.
func (p *parser) number(tok string, off int) (any, error) {
	bad := func(why string) error {
		return p.err(off, "%q is not a valid value: %s", tok, why)
	}
	s := tok
	signed := s[0] == '+' || s[0] == '-'
	if signed {
		s = s[1:]
	}
	if len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'o' || s[1] == 'b') {
		base, ok, name, article := 16, isHex, "hexadecimal", "a "
		switch s[1] {
		case 'o':
			base, ok, name, article = 8, isOct, "octal", "an "
		case 'b':
			base, ok, name, article = 2, isBin, "binary", "a "
		}
		if signed {
			return nil, bad(article + name + " integer cannot have a sign")
		}
		if !digitRun(s[2:], ok) {
			return nil, bad("invalid " + name + " digits")
		}
		n, err := strconv.ParseUint(strings.ReplaceAll(s[2:], "_", ""), base, 64)
		if err != nil || n > math.MaxInt64 {
			return nil, bad("it does not fit in a 64-bit signed integer")
		}
		return int64(n), nil
	}
	mant, exp, hasExp := strings.Cut(strings.ReplaceAll(s, "E", "e"), "e")
	whole, frac, hasFrac := strings.Cut(mant, ".")
	if !digitRun(whole, isDigit) {
		return nil, bad("strings must be quoted, and numbers are digits with single underscores between them")
	}
	if len(whole) > 1 && whole[0] == '0' {
		return nil, bad("leading zeros are not allowed")
	}
	clean := strings.ReplaceAll(tok, "_", "")
	if !hasFrac && !hasExp {
		n, err := strconv.ParseInt(clean, 10, 64)
		if err != nil {
			return nil, bad("it does not fit in a 64-bit signed integer")
		}
		return n, nil
	}
	if hasFrac && !digitRun(frac, isDigit) {
		return nil, bad("a decimal point must have digits on both sides")
	}
	if hasExp {
		e := exp
		if e != "" && (e[0] == '+' || e[0] == '-') {
			e = e[1:]
		}
		if !digitRun(e, isDigit) {
			return nil, bad("an exponent needs digits")
		}
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return nil, bad("it is out of range for a 64-bit float")
	}
	return f, nil
}

// fixed reads exactly n decimal digits from the start of s.
func fixed(s string, n int) (int, bool) {
	if len(s) < n {
		return 0, false
	}
	v := 0
	for i := 0; i < n; i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

// dateTime reads an offset date-time, local date-time, local date or local time.
func (p *parser) dateTime(tok string, off int) (any, error) {
	bad := func(why string) error {
		return p.err(off, "%q is not a valid date or time: %s", tok, why)
	}
	const form = "expected a form like 1979-05-27T07:32:00Z, 1979-05-27 or 07:32:00"
	if tok[2] == ':' {
		t, rest, why := parseClock(tok)
		if why == "" && rest != "" {
			why = form
		}
		if why != "" {
			return nil, bad(why)
		}
		return t, nil
	}
	d, why := parseDate(tok)
	if why != "" {
		return nil, bad(why)
	}
	if len(tok) == 10 {
		return d, nil
	}
	if c := tok[10]; c != 'T' && c != 't' && c != ' ' {
		return nil, bad(form)
	}
	t, rest, why := parseClock(tok[11:])
	if why != "" {
		return nil, bad(why)
	}
	if rest == "" {
		return LocalDateTime{Date: d, Time: t}, nil
	}
	var loc *time.Location
	switch {
	case rest == "Z" || rest == "z":
		loc = time.UTC
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		h, ok1 := fixed(rest[1:], 2)
		m, ok2 := fixed(rest[4:], 2)
		if !ok1 || !ok2 || h > 23 || m > 59 {
			return nil, bad("the offset must be +HH:MM or -HH:MM with HH up to 23 and MM up to 59")
		}
		secs := (h*60 + m) * 60
		if rest[0] == '-' {
			secs = -secs
		}
		loc = time.FixedZone("", secs)
	default:
		return nil, bad("the offset must be Z, +HH:MM or -HH:MM")
	}
	return time.Date(d.Year, d.Month, d.Day, t.Hour, t.Minute, t.Second, t.Nanosecond, loc), nil
}

func parseDate(s string) (LocalDate, string) {
	y, ok1 := fixed(s, 4)
	m, ok2 := fixed(s[5:], 2)
	d, ok3 := fixed(s[min(len(s), 8):], 2)
	if !ok1 || !ok2 || !ok3 || s[7] != '-' {
		return LocalDate{}, "expected a date as YYYY-MM-DD"
	}
	if m < 1 || m > 12 {
		return LocalDate{}, "the month must be 01 to 12"
	}
	if d < 1 || d > daysIn(y, time.Month(m)) {
		return LocalDate{}, fmt.Sprintf("%04d-%02d has no day %02d", y, m, d)
	}
	return LocalDate{Year: y, Month: time.Month(m), Day: d}, ""
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// parseClock reads HH:MM:SS[.fraction] and returns what follows it.
func parseClock(s string) (LocalTime, string, string) {
	h, ok1 := fixed(s, 2)
	m, ok2 := fixed(s[min(len(s), 3):], 2)
	sec, ok3 := fixed(s[min(len(s), 6):], 2)
	if !ok1 || !ok2 || !ok3 || s[2] != ':' || s[5] != ':' {
		return LocalTime{}, "", "expected a time as HH:MM:SS (seconds are required)"
	}
	if h > 23 || m > 59 || sec > 59 {
		return LocalTime{}, "", "the hour must be 00 to 23, and minutes and seconds 00 to 59"
	}
	t := LocalTime{Hour: h, Minute: m, Second: sec}
	rest := s[8:]
	if rest != "" && rest[0] == '.' {
		n := 1
		for n < len(rest) && isDigit(rest[n]) {
			n++
		}
		if n == 1 {
			return LocalTime{}, "", "a decimal point in the seconds must be followed by digits"
		}
		digits := rest[1:n]
		if len(digits) > 9 {
			digits = digits[:9] // finer than a nanosecond is truncated
		}
		ns, _ := strconv.Atoi(digits + strings.Repeat("0", 9-len(digits)))
		t.Nanosecond = ns
		rest = rest[n:]
	}
	return t, rest, ""
}

// basicString reads "..." with escapes.
func (p *parser) basicString() (string, error) {
	start := p.i
	p.i++
	var b []byte
	seg := p.i
	for {
		if p.eof() {
			return "", p.err(start, "this string is never closed")
		}
		switch c := p.data[p.i]; {
		case c == '"':
			s := p.data[seg:p.i]
			p.i++
			if b == nil {
				return string(s), nil
			}
			return string(append(b, s...)), nil
		case c == '\\':
			b = append(b, p.data[seg:p.i]...)
			var err error
			if b, err = p.escape(b); err != nil {
				return "", err
			}
			seg = p.i
		case c == '\n' || c == '\r':
			return "", p.err(start, `this string is never closed on its line; use """ for a string that spans lines`)
		case isControl(c):
			return "", p.err(p.i, "control character U+%04X must be escaped in a string", c)
		default:
			p.i++
		}
	}
}

// escape decodes the escape sequence at the cursor and appends it to b.
func (p *parser) escape(b []byte) ([]byte, error) {
	start := p.i
	p.i++
	if p.eof() {
		return nil, p.err(start, "this string is never closed")
	}
	c := p.data[p.i]
	p.i++
	switch c {
	case 'b':
		return append(b, '\b'), nil
	case 't':
		return append(b, '\t'), nil
	case 'n':
		return append(b, '\n'), nil
	case 'f':
		return append(b, '\f'), nil
	case 'r':
		return append(b, '\r'), nil
	case '"':
		return append(b, '"'), nil
	case '\\':
		return append(b, '\\'), nil
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if p.i+n > len(p.data) || !allHex(p.data[p.i:p.i+n]) {
			return nil, p.err(start, `\%c must be followed by %d hexadecimal digits`, c, n)
		}
		r, _ := strconv.ParseUint(string(p.data[p.i:p.i+n]), 16, 32)
		p.i += n
		if r > utf8.MaxRune || r >= 0xd800 && r <= 0xdfff {
			return nil, p.err(start, "%s is not a Unicode scalar value", p.data[start:p.i])
		}
		return utf8.AppendRune(b, rune(r)), nil
	}
	return nil, p.err(start, `invalid escape sequence: a backslash followed by %s (a literal backslash is written \\)`, p.describe(start+1))
}

func allHex(b []byte) bool {
	for _, c := range b {
		if !isHex(c) {
			return false
		}
	}
	return true
}

// quoteRun counts the quote characters q starting at the cursor and closes a
// multi-line string if there are three or more. Up to two may belong to the
// content, just before the closing three.
func (p *parser) quoteRun(q byte, b []byte) ([]byte, bool, error) {
	n := 0
	for p.i+n < len(p.data) && p.data[p.i+n] == q {
		n++
	}
	if n < 3 {
		p.i += n
		return append(b, p.data[p.i-n:p.i]...), false, nil
	}
	if n > 5 {
		return nil, false, p.err(p.i+5, "too many quotes: a multi-line string can hold at most two in a row before it closes")
	}
	b = append(b, p.data[p.i:p.i+n-3]...)
	p.i += n
	return b, true, nil
}

// skipFirstNewline drops a newline directly after an opening delimiter.
func (p *parser) skipFirstNewline() {
	if bytes.HasPrefix(p.data[p.i:], []byte("\n")) {
		p.i++
	} else if bytes.HasPrefix(p.data[p.i:], []byte("\r\n")) {
		p.i += 2
	}
}

// multiBasic reads """...""".
func (p *parser) multiBasic() (string, error) {
	start := p.i
	p.i += 3
	p.skipFirstNewline()
	b := []byte{}
	for {
		if p.eof() {
			return "", p.err(start, `this multi-line string is never closed with """`)
		}
		var err error
		var done bool
		switch c := p.data[p.i]; {
		case c == '"':
			b, done, err = p.quoteRun('"', b)
			if done {
				return string(b), nil
			}
		case c == '\\':
			j := p.i + 1
			for j < len(p.data) && (p.data[j] == ' ' || p.data[j] == '\t') {
				j++
			}
			if j < len(p.data) && (p.data[j] == '\n' || p.data[j] == '\r') {
				// A line-ending backslash: drop it and all whitespace up to
				// the next visible character.
				p.i = j
				for !p.eof() && (p.data[p.i] == ' ' || p.data[p.i] == '\t' || p.data[p.i] == '\n' || p.data[p.i] == '\r') {
					if p.data[p.i] == '\r' {
						if err = p.newline(); err != nil {
							return "", err
						}
						continue
					}
					p.i++
				}
				continue
			}
			b, err = p.escape(b)
		case c == '\r':
			s := p.i
			if err = p.newline(); err == nil {
				b = append(b, p.data[s:p.i]...)
			}
		case c == '\n':
			b = append(b, c)
			p.i++
		case isControl(c):
			err = p.err(p.i, "control character U+%04X must be escaped in a string", c)
		default:
			b = append(b, c)
			p.i++
		}
		if err != nil {
			return "", err
		}
	}
}

// literalString reads '...', which has no escapes.
func (p *parser) literalString() (string, error) {
	start := p.i
	for p.i++; ; p.i++ {
		if p.eof() {
			return "", p.err(start, "this string is never closed")
		}
		switch c := p.data[p.i]; {
		case c == '\'':
			p.i++
			return string(p.data[start+1 : p.i-1]), nil
		case c == '\n' || c == '\r':
			return "", p.err(start, "this string is never closed on its line; use ''' for a string that spans lines")
		case isControl(c):
			return "", p.err(p.i, "control character U+%04X is not allowed in a literal string", c)
		}
	}
}

// multiLiteral reads ”'...”'.
func (p *parser) multiLiteral() (string, error) {
	start := p.i
	p.i += 3
	p.skipFirstNewline()
	b := []byte{}
	for {
		if p.eof() {
			return "", p.err(start, "this multi-line string is never closed with '''")
		}
		var err error
		var done bool
		switch c := p.data[p.i]; {
		case c == '\'':
			b, done, err = p.quoteRun('\'', b)
			if done {
				return string(b), nil
			}
		case c == '\r':
			s := p.i
			if err = p.newline(); err == nil {
				b = append(b, p.data[s:p.i]...)
			}
		case c == '\n':
			b = append(b, c)
			p.i++
		case isControl(c):
			err = p.err(p.i, "control character U+%04X is not allowed in a literal string", c)
		default:
			b = append(b, c)
			p.i++
		}
		if err != nil {
			return "", err
		}
	}
}

// array reads [ v, v, ... ]; newlines and comments may appear between values.
func (p *parser) array() (value, error) {
	start := p.i
	v := value{pos: start, kind: vArray}
	p.i++
	for {
		if err := p.skipBlank(); err != nil {
			return v, err
		}
		if p.eof() {
			return v, p.err(start, "this array is never closed with ']'")
		}
		if p.data[p.i] == ']' {
			p.i++
			return v, nil
		}
		e, err := p.value()
		if err != nil {
			return v, err
		}
		v.a = append(v.a, e)
		if err := p.skipBlank(); err != nil {
			return v, err
		}
		if p.eof() {
			return v, p.err(start, "this array is never closed with ']'")
		}
		switch p.data[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return v, nil
		default:
			return v, p.err(p.i, "expected ',' or ']' after an array element, found %s", p.describe(p.i))
		}
	}
}

// inlineTable reads { k = v, ... } on a single line.
func (p *parser) inlineTable() (value, error) {
	start := p.i
	t := &table{pos: start, kind: tInline}
	v := value{pos: start, kind: vTable, t: t}
	p.i++
	p.skipWS()
	if !p.eof() && p.data[p.i] == '}' {
		p.i++
		return v, nil
	}
	for {
		if err := p.keyval(t); err != nil {
			return v, err
		}
		p.skipWS()
		if p.eof() {
			return v, p.err(start, "this inline table is never closed with '}'")
		}
		switch c := p.data[p.i]; c {
		case ',':
			comma := p.i
			p.i++
			p.skipWS()
			if !p.eof() && p.data[p.i] == '}' {
				return v, p.err(comma, "an inline table cannot end with a comma")
			}
		case '}':
			p.i++
			freeze(t)
			return v, nil
		case '\n', '\r':
			return v, p.err(p.i, "an inline table must be on a single line; use a [table] header for more keys")
		default:
			return v, p.err(p.i, "expected ',' or '}' in the inline table, found %s", p.describe(p.i))
		}
	}
}

// freeze closes an inline table and every table its dotted keys created, so
// nothing after it can add keys.
func freeze(t *table) {
	t.kind = tInline
	for _, v := range t.vals {
		if v.kind == vTable {
			freeze(v.t)
		}
	}
}

// fmtKey writes a key path as TOML would, quoting parts that are not bare.
func fmtKey(parts []string) string {
	var b strings.Builder
	for i, k := range parts {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(keyText(k))
	}
	return b.String()
}

func keyText(k string) string {
	if k == "" {
		return `""`
	}
	for i := 0; i < len(k); i++ {
		if !isBare(k[i]) {
			return strconv.Quote(k)
		}
	}
	return k
}
