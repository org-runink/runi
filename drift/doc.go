// SPDX-License-Identifier: BSD-3-Clause

// Package drift tells you when a stream of measurements has changed regime.
//
// It is for series read from a running system over and over — a pressure
// stall percentage every second, a latency per request, a queue depth per
// scrape — where the question is "has this moved?" and the answer is needed
// as each reading arrives. Every type does O(1) work and holds fixed memory
// per update, allocates nothing, and is a plain value, so one detector per
// series is affordable for thousands of series.
//
// Which to use:
//
//   - Welford and EWMA describe. Welford is the exact running mean and
//     variance, numerically stable and mergeable across shards; EWMA is a
//     smoothed level that forgets. Neither decides anything. Welford over a
//     known-good stretch is how to get the target and σ the detectors need.
//   - EWMAChart catches small shifts that persist — half a standard deviation,
//     sustained — that a check on raw values never sees, and is a good
//     general choice when the size of the shift you care about is unknown.
//   - CUSUM catches a shift of a size you name, and for that size it is the
//     fastest detector there is at a given false-alarm rate. It also estimates
//     when the shift began (Since).
//   - PageHinkley is CUSUM for when you have no target: the reference level is
//     the series' own running mean.
//   - Decayed is not a detector. It turns a cumulative counter (time stalled,
//     bytes sent) read at irregular times into a decayed rate — the "average
//     over the last 10/60/300 s" that operating systems publish — which is
//     usually the series you then want to watch.
//
// The detectors return a Signal: Up, Down or None, for the sample just added.
// A signal is a statistic crossing a threshold you chose; what follows from it
// is your code's decision, and the detector carries on accumulating until you
// call Reset.
//
//	c, _ := drift.DefaultCUSUM(target, sigma)
//	for x := range readings {
//		if s := c.Add(x); s != drift.None {
//			log.Printf("%s shift began %d samples ago", s, c.Since())
//			c.Reset()
//		}
//	}
//
// Every type skips NaN and ±Inf rather than folding them in, and counts them:
// a missed reading should not poison a statistic for the rest of the series,
// and a series that is mostly NaN should be visible as one (Skipped). None is
// safe for concurrent use; give each goroutine its own, or guard it.
//
// # What this package does not do
//
// It does not choose thresholds. The defaults offered (DefaultCUSUM,
// DefaultEWMAChart) are the textbook values, with the false-alarm rates the
// textbooks give for them, stated in their docs and checked by tests. Whether
// a false alarm every 465 samples suits a series read every second is a
// question about your system, not about statistics.
//
// It does not know about seasons. A daily cycle is, to every detector here, a
// shift up every morning and down every night. Remove the cycle first — the
// residual from github.com/org-runink/runi/season is the series to watch —
// or compare against the same hour of previous days yourself.
//
// It is univariate. Each detector watches one series. Shifts that show only
// in the relationship between series (two latencies that normally move
// together diverging) need a multivariate chart such as Hotelling's T² or
// MEWMA, which this package does not provide.
//
// Signals are not probabilities. A CUSUM over h says the evidence crossed h;
// it does not say "99% sure". The in-control average run lengths in the docs
// are the honest way to read the rate of false alarms, and they assume
// independent samples: on autocorrelated data every detector here signals
// more often than its documented rate. Model the autocorrelation away first
// (github.com/org-runink/runi/arimax residuals) if it is strong.
//
// It never acts. Nothing here alerts, throttles, restarts or retries; it
// returns numbers.
package drift
