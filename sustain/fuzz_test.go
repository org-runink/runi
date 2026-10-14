package sustain

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// fuzzReadings decodes b as 10-byte records: a little-endian float64 value,
// so every bit pattern is reachable (NaN, infinities, subnormals, the largest
// finite values), then a signed 16-bit time step in milliseconds, so
// duplicates and out-of-order times are as easy to reach as ordered ones. The
// step's low bit is the Debounce condition.
func fuzzReadings(b []byte) []reading {
	var rs []reading
	at := t0
	for i := 0; i+10 <= len(b) && len(rs) < 256; i += 10 {
		x := math.Float64frombits(binary.LittleEndian.Uint64(b[i:]))
		step := int16(binary.LittleEndian.Uint16(b[i+8:]))
		at = at.Add(time.Duration(step) * time.Millisecond)
		rs = append(rs, reading{x: x, cond: step&1 != 0, at: at})
	}
	return rs
}

func record(x float64, stepMs int16) []byte {
	b := make([]byte, 10)
	binary.LittleEndian.PutUint64(b, math.Float64bits(x))
	binary.LittleEndian.PutUint16(b[8:], uint16(stepMs))
	return b
}

func records(rs ...[]byte) []byte {
	var b []byte
	for _, r := range rs {
		b = append(b, r...)
	}
	return b
}

// Every type agrees with its naive reference for any readings and any
// parameters its constructor accepts, and every constructor accepts exactly
// the parameters its doc names. Slew is held to its bound rather than to a
// reference alone, since with extreme values the bound is the claim.
func FuzzSustain(f *testing.F) {
	f.Add(records(record(1, 1000), record(5, 1000), record(-3, 1001), record(5, -500), record(math.NaN(), 7)), 2.0, 0.0, uint8(2), uint8(3), uint16(1500), false)
	f.Add(records(record(math.Inf(1), 1), record(math.MaxFloat64, 1), record(-math.MaxFloat64, 1), record(0, 0)), 1e308, -1e308, uint8(1), uint8(64), uint16(0), true)
	f.Add(records(record(0.5, 3), record(0.5, 3), record(0.4, -1), record(0.6, 2)), 0.5, 0.5, uint8(0), uint8(0), uint16(1), true)
	f.Add(records(record(math.SmallestNonzeroFloat64, 9), record(-0.0, 9), record(1e-300, 9)), 1e-300, -1e-300, uint8(5), uint8(4), uint16(65535), false)
	f.Fuzz(func(t *testing.T, b []byte, p, q float64, n, m uint8, dwellMs uint16, low bool) {
		rs := fuzzReadings(b)
		accX, _ := accepted(rs, true)
		accC, _ := accepted(rs, false)
		dwell := time.Duration(dwellMs) * time.Millisecond
		dir := High
		if low {
			dir = Low
		}

		set, clear := max(p, q), min(p, q)
		if low {
			set, clear = clear, set
		}
		h, err := NewHysteresis(dir, set, clear, dwell)
		if valid := finite(p) && finite(q) && p != q; (err == nil) != valid {
			t.Fatalf("NewHysteresis(%v, %v, %v): err %v, want valid %v", dir, set, clear, err, valid)
		}
		if err == nil {
			want := refHysteresis(dir, set, clear, dwell, accX)
			k := 0
			for _, r := range rs {
				before := h.Skipped()
				on := h.Update(r.x, r.at)
				if h.Skipped() == before {
					if b2i(on) != want[k] {
						t.Fatalf("Hysteresis reading %d: %v, want %d", k, on, want[k])
					}
					k++
				}
			}
		}

		edges := []Edge{{Up: max(p, q), Down: min(p, q)}, {Up: max(p, q) + 1, Down: min(p, q) + 1}}
		bd, err := NewBand(dwell, edges...)
		valid := finite(p) && finite(q) && p != q && edges[1].Down < edges[1].Up && edges[1].Up > edges[0].Up && edges[1].Down > edges[0].Down
		if (err == nil) != valid {
			t.Fatalf("NewBand(%v): err %v, want valid %v", edges, err, valid)
		}
		if err == nil {
			want := refBand(edges, dwell, accX)
			k := 0
			for _, r := range rs {
				before := bd.Skipped()
				l := bd.Update(r.x, r.at)
				if bd.Skipped() == before {
					if l != want[k] {
						t.Fatalf("Band reading %d: %d, want %d", k, l, want[k])
					}
					k++
				}
			}
		}

		w, err := NewNofM(dir, p, int(n), int(m))
		if valid := finite(p) && m >= 1 && m <= 64 && n >= 1 && n <= m; (err == nil) != valid {
			t.Fatalf("NewNofM(%v, %d, %d): err %v, want valid %v", p, n, m, err, valid)
		}
		if err == nil {
			want := refNofM(dir, p, int(n), int(m), accX)
			k := 0
			for _, r := range rs {
				before := w.Skipped()
				on := w.Update(r.x, r.at)
				if w.Skipped() == before {
					if b2i(on) != want[k] {
						t.Fatalf("NofM reading %d: %v, want %d", k, on, want[k])
					}
					k++
				}
			}
		}

		on := Delay{For: dwell, Readings: int(n % 8)}
		off := Delay{For: dwell / 2, Readings: int(m % 8)}
		d, err := NewDebounce(on, off)
		if err != nil {
			t.Fatal(err)
		}
		want := refDebounce(on, off, accC)
		k := 0
		for _, r := range rs {
			before := d.Skipped()
			st := d.Update(r.cond, r.at)
			if d.Skipped() == before {
				if b2i(st) != want[k] {
					t.Fatalf("Debounce reading %d: %v, want %d", k, st, want[k])
				}
				k++
			}
		}

		rise, fall := math.Abs(p), math.Abs(q)
		s, err := NewSlew(rise, fall)
		if valid := rise > 0 && fall > 0; (err == nil) != valid {
			t.Fatalf("NewSlew(%v, %v): err %v, want valid %v", rise, fall, err, valid)
		}
		if err == nil {
			ws := refSlew(rise, fall, accX)
			k, y, last := 0, 0.0, time.Time{}
			for _, r := range rs {
				before := s.Skipped()
				got := s.Update(r.x, r.at)
				if s.Skipped() > before {
					continue
				}
				if got != ws[k] {
					t.Fatalf("Slew reading %d: %v, want %v", k, got, ws[k])
				}
				if k > 0 {
					dt := r.at.Sub(last).Seconds()
					if got > y+rise*dt || got < y-fall*dt || (got-y)*(r.x-got) < 0 {
						t.Fatalf("Slew %v → %v toward %v over %vs: bound broken", y, got, r.x, dt)
					}
				}
				k, y, last = k+1, got, r.at
			}
		}
	})
}
