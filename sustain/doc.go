// SPDX-License-Identifier: BSD-3-Clause

// Package sustain keeps a decision made on a noisy series from flapping
// around its threshold.
//
// A check of the form "x > limit" run on every reading of a live series
// changes its answer every time noise carries x across the limit, and
// whatever the answer drives — an alert, a throttle, a scale-up — changes
// with it. The remedies are old and well understood: two levels instead of
// one, a condition that must persist before it counts, a majority over the
// last few readings, a bound on how fast an output may move. This package
// provides each as a small value type that takes one reading at a time,
// with the time the reading was taken.
//
// Which to use:
//
//   - Hysteresis is an on/off state with a set level and a separate clear
//     level (a Schmitt trigger), High or Low, and an optional minimum dwell
//     in a state after each change. The deadband between the levels absorbs
//     noise smaller than itself.
//   - Debounce follows a boolean condition only after it has held for a time
//     and/or a number of consecutive readings, with separate delays for on
//     and off: the on-delay and off-delay of alarm management.
//   - NofM is on while at least Need of the last Size readings (Size ≤ 64)
//     are past a level: it escalates on a condition that holds most of the
//     time, which a Debounce would keep restarting.
//   - Band classifies a reading into ordered levels — below, inside, above;
//     normal, warning, critical — with a deadband on every edge.
//   - Slew bounds how fast an output follows its input, in units per second
//     of the readings' own time, with separate rates up and down.
//
// They compose. Debounce takes any condition, so a Hysteresis's state or a
// Band's level compared with a constant can feed it; a Slew in front of any
// of them smooths what they see, and one behind a controller smooths what it
// does.
//
//	h, _ := sustain.NewHysteresis(sustain.High, 90, 80, time.Minute)
//	d, _ := sustain.NewDebounce(sustain.Delay{For: 30 * time.Second}, sustain.Delay{})
//	for r := range readings {
//		if d.Update(h.Update(r.Value, r.At), r.At) {
//			log.Printf("hot for %v", d.InState(r.At))
//		}
//	}
//
// Time comes only from the at argument of each Update; nothing here reads
// the clock or sleeps, so every type is as testable as the caller's clock
// is. Times are compared with time.Time.After and differenced with
// time.Time.Sub, which use the monotonic reading time.Now attaches, so a
// wall-clock step between two such readings does not distort an interval.
//
// Two rules hold for every type, and tests hold each type to them:
//
//   - A reading whose time is not after the last accepted reading — a
//     duplicate, a clock stepped back, readings delivered out of order — is
//     skipped. There is no right place to put a late reading in a state
//     machine that has already acted on later ones, and a duplicate would
//     count twice toward a number of readings.
//   - A NaN or ±Inf reading is skipped. A missed reading should not decide a
//     state, and "no reading" is not evidence for either side of a level.
//
// A skipped reading is counted in Skipped and leaves no other trace: the
// state, the time of the last reading and any run or window are exactly as
// if it had never been offered. A series that is mostly skipped readings is
// visible as one.
//
// Every Update does O(1) work in fixed memory — Band's is O(levels crossed)
// — and allocates nothing; a test holds each to zero. Every type is safe to
// copy and none is safe for concurrent use; give each goroutine its own, or
// guard it. Constructor errors wrap ErrInvalid.
//
// # What this package does not do
//
// It does not choose levels, deadbands or delays. Those depend on the
// series' noise and on what a late or a false decision costs, which only the
// caller knows. ISA-18.2 and its technical report on alarm design tabulate
// starting deadbands and delays by measurement type for process plant
// signals; a deadband a few times the noise's standard deviation, measured
// over a stretch known to be normal (Welford in
// github.com/org-runink/runi/drift), is the usual first try elsewhere.
//
// It does not detect change. A deadband stops flapping around a level you
// chose; whether the series' level has itself moved is a question for the
// detectors in github.com/org-runink/runi/drift, whose output can then drive
// a Debounce here.
//
// It sees only readings. A Debounce's "held for 30 s" is "every reading for
// 30 s agreed"; a gap between readings is not seen, so a run of two readings
// a minute apart has lasted a minute. A caller who needs to treat a long gap
// as breaking a run should check it (Intervals in
// github.com/org-runink/runi/rate watches gaps) and call Reset.
//
// It does not reorder or interpolate. A late reading is dropped, not placed;
// a Slew's output exists only at readings, and nothing between them is
// computed.
//
// It never acts. Nothing here alerts, throttles or retries; each type
// returns a state or a number, and what follows from it is the caller's.
package sustain
