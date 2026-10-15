---
title: "runi/drift"
weight: 13
---

```go
import "github.com/org-runink/runi/drift"
```

Package drift tells you when a stream of measurements has changed regime.

It is for series read from a running system over and over — a pressure
stall percentage every second, a latency per request, a queue depth per
scrape — where the question is "has this moved?" and the answer is needed
as each reading arrives. Every type does O(1) work and holds fixed memory
per update, allocates nothing, and is a plain value, so one detector per
series is affordable for thousands of series.

Which to use:

  - Welford and EWMA describe. Welford is the exact running mean and
    variance, numerically stable and mergeable across shards; EWMA is a
    smoothed level that forgets. Neither decides anything. Welford over a
    known-good stretch is how to get the target and σ the detectors need.
  - EWMAChart catches small shifts that persist — half a standard deviation,
    sustained — that a check on raw values never sees, and is a good
    general choice when the size of the shift you care about is unknown.
  - CUSUM catches a shift of a size you name, and for that size it is the
    fastest detector there is at a given false-alarm rate. It also estimates
    when the shift began (Since).
  - PageHinkley is CUSUM for when you have no target: the reference level is
    the series' own running mean.
  - Decayed is not a detector. It turns a cumulative counter (time stalled,
    bytes sent) read at irregular times into a decayed rate — the "average
    over the last 10/60/300 s" that operating systems publish — which is
    usually the series you then want to watch.

The detectors return a Signal: Up, Down or None, for the sample just added.
A signal is a statistic crossing a threshold you chose; what follows from it
is your code's decision, and the detector carries on accumulating until you
call Reset.

c, _ := drift.DefaultCUSUM(target, sigma)
for x := range readings {
	if s := c.Add(x); s != drift.None {
		log.Printf("%s shift began %d samples ago", s, c.Since())
		c.Reset()
	}
}

Every type skips NaN and ±Inf rather than folding them in, and counts them:
a missed reading should not poison a statistic for the rest of the series,
and a series that is mostly NaN should be visible as one (Skipped). None is
safe for concurrent use; give each goroutine its own, or guard it.

# What this package does not do

It does not choose thresholds. The defaults offered (DefaultCUSUM,
DefaultEWMAChart) are the textbook values, with the false-alarm rates the
textbooks give for them, stated in their docs and checked by tests. Whether
a false alarm every 465 samples suits a series read every second is a
question about your system, not about statistics.

It does not know about seasons. A daily cycle is, to every detector here, a
shift up every morning and down every night. Remove the cycle first — the
residual from github.com/org-runink/runi/season is the series to watch —
or compare against the same hour of previous days yourself.

It is univariate. Each detector watches one series. Shifts that show only
in the relationship between series (two latencies that normally move
together diverging) need a multivariate chart such as Hotelling's T² or
MEWMA, which this package does not provide.

Signals are not probabilities. A CUSUM over h says the evidence crossed h;
it does not say "99% sure". The in-control average run lengths in the docs
are the honest way to read the rate of false alarms, and they assume
independent samples: on autocorrelated data every detector here signals
more often than its documented rate. Model the autocorrelation away first
(github.com/org-runink/runi/arimax residuals) if it is strong.

It never acts. Nothing here alerts, throttles, restarts or retries; it
returns numbers.

## Index

- `type` [CUSUM](#cusum)
- `type` [Decayed](#decayed)
- `type` [EWMA](#ewma)
- `type` [EWMAChart](#ewmachart)
- `type` [PageHinkley](#pagehinkley)
- `type` [Signal](#signal)
- `type` [Welford](#welford)
- `method` [CUSUM.Add](#cusumadd)
- `method` [CUSUM.Lower](#cusumlower)
- `method` [CUSUM.N](#cusumn)
- `method` [CUSUM.Reset](#cusumreset)
- `method` [CUSUM.Since](#cusumsince)
- `method` [CUSUM.Skipped](#cusumskipped)
- `method` [CUSUM.Upper](#cusumupper)
- `method` [Decayed.Reset](#decayedreset)
- `method` [Decayed.Resets](#decayedresets)
- `method` [Decayed.Skipped](#decayedskipped)
- `method` [Decayed.Update](#decayedupdate)
- `method` [Decayed.Value](#decayedvalue)
- `method` [Decayed.Window](#decayedwindow)
- `func` [DefaultCUSUM](#defaultcusum)
- `func` [DefaultEWMAChart](#defaultewmachart)
- `const` [ErrInvalid](#errinvalid)
- `method` [EWMA.Add](#ewmaadd)
- `method` [EWMA.Alpha](#ewmaalpha)
- `method` [EWMA.N](#ewman)
- `method` [EWMA.Reset](#ewmareset)
- `method` [EWMA.Skipped](#ewmaskipped)
- `method` [EWMA.Value](#ewmavalue)
- `method` [EWMAChart.Add](#ewmachartadd)
- `method` [EWMAChart.Limits](#ewmachartlimits)
- `method` [EWMAChart.N](#ewmachartn)
- `method` [EWMAChart.Reset](#ewmachartreset)
- `method` [EWMAChart.Skipped](#ewmachartskipped)
- `method` [EWMAChart.Statistic](#ewmachartstatistic)
- `func` [NewCUSUM](#newcusum)
- `func` [NewDecayed](#newdecayed)
- `func` [NewEWMA](#newewma)
- `func` [NewEWMAChart](#newewmachart)
- `func` [NewEWMAHalfLife](#newewmahalflife)
- `func` [NewPageHinkley](#newpagehinkley)
- `method` [PageHinkley.Add](#pagehinkleyadd)
- `method` [PageHinkley.N](#pagehinkleyn)
- `method` [PageHinkley.Reset](#pagehinkleyreset)
- `method` [PageHinkley.Skipped](#pagehinkleyskipped)
- `method` [PageHinkley.Statistic](#pagehinkleystatistic)
- `method` [Signal.String](#signalstring)
- `method` [Welford.Add](#welfordadd)
- `method` [Welford.Mean](#welfordmean)
- `method` [Welford.Merge](#welfordmerge)
- `method` [Welford.N](#welfordn)
- `method` [Welford.PopVariance](#welfordpopvariance)
- `method` [Welford.Reset](#welfordreset)
- `method` [Welford.Skipped](#welfordskipped)
- `method` [Welford.StdDev](#welfordstddev)
- `method` [Welford.Variance](#welfordvariance)

### CUSUM

```go
type CUSUM struct
```

CUSUM is the two-sided tabular cumulative-sum test for a shift in the mean
away from a known target.

Each side accumulates the evidence for a shift in its direction and is
floored at zero, so that time spent in control is not credited against a
later shift:

S⁺_t = max(0, S⁺_{t−1} + x_t − target − k)
S⁻_t = max(0, S⁻_{t−1} + target − x_t − k)

and a side signals once it exceeds h. k is the slack: deviations smaller
than k drain the sum rather than feed it. Set to half the shift you want to
catch, the test is the fastest possible detector for a shift of exactly
that size among those with the same false-alarm rate, in the worst-case
sense of Lorden (G. V. Moustakides, "Optimal stopping times for detecting
changes in distributions", Annals of Statistics 14(4), 1986). For shifts
much smaller than 2k it is slow, and EWMAChart is the better choice when
the size is unknown.

Since estimates when the shift began: a side becomes positive at about the
sample the shift started, so the run of samples for which it has been
positive is the age of the shift. This is Montgomery's estimate, and it
costs nothing to keep.

Method: E. S. Page, "Continuous inspection schemes", Biometrika 41(1/2),
1954. Tabular form, the Since estimate and the run-length tables quoted
below: D. C. Montgomery, Introduction to Statistical Quality Control,
Wiley, chapter on the cumulative sum control chart.

k and h are in the data's units. A signal does not reset the statistics:
they keep accumulating, and keep signalling while the shift lasts, until
you call Reset. Whether to reset, re-estimate the target, or simply note
the change is the caller's decision. Like EWMAChart, it assumes independent
samples with a known in-control mean.

The zero value is not usable; build one with NewCUSUM or DefaultCUSUM. Safe
to copy, not safe for concurrent use. NaN and ±Inf are skipped and counted
in Skipped. Each side is capped at the largest finite float64, so no finite
input, however large, can turn the statistics into Inf or NaN.

<small>Defined in <code>drift/cusum.go</code>.</small>

### Decayed

```go
type Decayed struct
```

Decayed turns a cumulative counter, read at irregular times, into an
exponentially decayed average rate.

Many system statistics are published as running totals rather than rates:
microseconds spent stalled on memory or I/O since boot, bytes sent, requests
served. What a reader wants is "the share of time stalled over the last
minute" or "requests per second lately", which is what operating systems
report alongside such totals as 10, 60 and 300 second averages. Decayed
computes that from the totals alone. For a counter of microseconds stalled,
the rate is microseconds per second, so dividing by 1e6 gives the share of
time (multiply by 100 for a percentage).

Each Update takes the rate over the interval since the last one,
r = Δtotal/Δt, and blends it into the average with

e = exp(−Δt/window),   a = a·e + r·(1−e).

This is exact for irregular sampling: it is the continuous-time exponential
average with time constant window of a rate that was r throughout the
interval, which is all a pair of counter readings can say about it. A fixed
per-sample weight, as in EWMA, would be wrong under jitter: a reading
delayed to 3 s would count the same as one taken after 1 s, and a burst of
readings close together would drag the average towards whatever those
moments happened to show. Here a reading's weight is the time it covers.

After one window of a new constant rate the average has moved 1 − 1/e, about
63%, of the way from the old rate to the new one, whatever the spacing of the
readings; after three windows, 95%. The first rate initialises the average
directly rather than being blended into an implicit zero, which would read
as a quiet system for the first window and then as a rise that never
happened.

A total lower than the previous one means the counter was reset, or wrapped
past its maximum. With one reading the two cannot be told apart, so both are
treated as a reset: the reading becomes a new baseline, no rate is produced,
and Resets is incremented. A counter that wraps loses one interval, never
yields an absurd rate.

Time comes only from the at argument; nothing here reads the clock, so it
is as testable as the caller's clock is. Intervals are taken with
time.Time.Sub, which uses the monotonic reading that time.Now attaches, so a
wall-clock step between two such readings does not distort the interval.
Readings at or before the previous one (a clock stepped backwards, a
duplicate) are skipped and counted in Skipped, and leave the baseline where
it was: the next valid reading covers their interval.

Method: the exponential smoothing of irregularly spaced data of D. J. Wright,
"Forecasting data published at irregular time intervals using an extension
of Holt's method", Management Science 32(4), 1986, here applied to the
interval-average rate, which makes the step response exact rather than
approximate.

The zero value is not usable; build one with NewDecayed. Safe to copy, not
safe for concurrent use.

<small>Defined in <code>drift/decayed.go</code>.</small>

### EWMA

```go
type EWMA struct
```

EWMA is an exponentially weighted moving average: each new value moves the
average a fixed fraction alpha of the way towards itself,

v_t = v_{t−1} + alpha·(x_t − v_{t−1}),

so a value k samples old carries weight alpha·(1−alpha)^k. It describes a
level; it does not test one. For a decision about whether the level moved,
use EWMAChart, which adds the control limits this lacks.

The first value initialises the average rather than being blended with an
implicit zero, so a series that starts at 500 does not spend its first few
dozen samples climbing up from nothing. The average counts samples, not
time: for readings at irregular times, Decayed is the right tool.

Method: the exponentially weighted average as used in forecasting by R. G.
Brown, Statistical Forecasting for Inventory Control, McGraw-Hill, 1959.

The zero value is not usable (its alpha is 0); build one with NewEWMA or
NewEWMAHalfLife. Safe to copy, not safe for concurrent use. NaN and ±Inf are
skipped and counted in Skipped.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMAChart

```go
type EWMAChart struct
```

EWMAChart is an exponentially weighted moving average control chart: it
signals when a smoothed series leaves a band around a target level.

The statistic is

z_t = λ·x_t + (1−λ)·z_{t−1},   z_0 = target,

and the band at sample t is

target ± L·σ·sqrt( λ/(2−λ) · (1 − (1−λ)^(2t)) ).

σ times the square root is the exact standard deviation of z_t when the x_t
are independent with standard deviation σ. It starts at λσ and widens towards
the asymptotic σ·sqrt(λ/(2−λ)); using the asymptotic band from the first
sample would leave the chart nearly blind to a shift present from the start.

Use it for small shifts that persist. Averaging over about 2/λ samples lets
it see a shift of half a standard deviation that a chart of raw values
would never flag, at the price of reacting more slowly than a raw-value
check to a single large spike. For a shift of one known size CUSUM is the
faster detector; the two have similar performance across a range of sizes.

Method: S. W. Roberts, "Control chart tests based on geometric moving
averages", Technometrics 1(3), 1959. Exact-variance limits and run-length
tables: J. M. Lucas and M. S. Saccucci, "Exponentially weighted moving
average control schemes: properties and enhancements", Technometrics 32(1),
1990. Presentation follows D. C. Montgomery, Introduction to Statistical
Quality Control, Wiley, chapter on CUSUM and EWMA charts.

target and σ are the in-control mean and standard deviation of the series,
and they are yours to supply: estimate them from a stretch you know to be
normal (Welford does that), not from the stretch under test. The chart
assumes independent samples; a strongly autocorrelated series inflates the
spread of z_t beyond what the limits allow for and signals too often.

The zero value is not usable; build one with NewEWMAChart or
DefaultEWMAChart. Safe to copy, not safe for concurrent use. NaN and ±Inf
are skipped and counted in Skipped, and do not advance t. Finite inputs
within a factor of a few of the float64 limit can overflow z_t; rescale
such a series first.

<small>Defined in <code>drift/chart.go</code>.</small>

### PageHinkley

```go
type PageHinkley struct
```

PageHinkley is the Page–Hinkley test for a change in the mean of a series
whose level is not known in advance.

It is CUSUM with the target replaced by the running mean of everything seen
since the last Reset. With mean_t the mean of x_1..x_t, it keeps

m⁺_t = Σ (x_i − mean_i − δ),   M⁺_t = min(0, m⁺_1, …, m⁺_t)
m⁻_t = Σ (x_i − mean_i + δ),   M⁻_t = max(0, m⁻_1, …, m⁻_t)

and signals Up when m⁺_t − M⁺_t > λ and Down when M⁻_t − m⁻_t > λ (the
larger when both, Up on a tie). δ plays the part of CUSUM's k, the size of
deviation tolerated without accumulating evidence, and λ the part of h.

Use it when there is no target to supply: a series per process, per disk,
per tenant, each settling at its own level. The price is that the reference
level is learned from the series itself. Shortly after Reset it rests on a
handful of samples, so the test is noisier than CUSUM with a true target.
After a change the running mean slowly absorbs the new level, the evidence
stops growing and then drains, and the next change would be measured
against a blend of both regimes. Reset after acting on a signal. When the
target is known, CUSUM is the sharper test.

Method: E. S. Page, "Continuous inspection schemes", Biometrika 41(1/2),
1954; D. V. Hinkley, "Inference about the change-point from cumulative sum
tests", Biometrika 58(3), 1971. The running-mean formulation is the one
given by J. Gama, I. Žliobaitė, A. Bifet, M. Pechenizkiy and A. Bouchachia,
"A survey on concept drift adaptation", ACM Computing Surveys 46(4), 2014.

δ and λ are in the data's units and the caller's to choose. There is no
literature default worth the name, since both scale with the noise. δ =
σ/2 and λ = 5σ, the CUSUM defaults, are a reasonable start: at those values
the in-control average run length measured here is about 480 samples, close
to CUSUM's 465, and a one-sigma shift after 100 in-control samples is
detected in about 11 to 12 (TestPageHinkleyARL). The delay grows when the
shift comes early, while the mean still rests on few samples.

The zero value is not usable; build one with NewPageHinkley. Safe to copy,
not safe for concurrent use. NaN and ±Inf are skipped and counted in
Skipped. Finite inputs within a factor of a few of the float64 limit can
overflow the sums; rescale such a series first.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### Signal

```go
type Signal int
```

Signal is what a detector says about the sample it was just given.

It is a statistic crossing a threshold the caller chose, not a probability
and not a verdict: Up means "the evidence for an upward shift is now larger
than you said it had to be", and what to do about that is the caller's.

<small>Defined in <code>drift/signal.go</code>.</small>

### Welford

```go
type Welford struct
```

Welford is a running mean and variance in one pass and fixed memory.

The obvious one-pass method keeps Σx and Σx² and takes
(Σx² − (Σx)²/n)/(n−1) at the end. On data far from zero that subtracts two
nearly equal huge numbers, and the variance it returns is mostly rounding:
readings that move by a few units around 1e9 lose every significant digit
of their spread, and the result can even come out negative. Welford's
update keeps the mean and the sum of squared deviations from it instead,
so nothing large is ever subtracted from anything large.

Method: B. P. Welford, "Note on a method for calculating corrected sums of
squares and products", Technometrics 4(3), 1962. Merge is the pairwise
combination of T. F. Chan, G. H. Golub and R. J. LeVeque, "Updating
formulae and a pairwise algorithm for computing sample variances", Stanford
report STAN-CS-79-773, 1979, published as "Algorithms for computing the
sample variance: analysis and recommendations", The American Statistician
37(3), 1983.

The zero value is an empty accumulator, ready to use. A Welford is a plain
value and safe to copy, which is how a snapshot is taken; it is not safe
for concurrent use. NaN and ±Inf are skipped and counted in Skipped, never
folded in: one NaN would otherwise poison the mean for the rest of the
series. Finite values whose squared spread overflows float64 (beyond about
1e154) give a variance of +Inf, never NaN or a negative number; the mean
stays finite. Rescale such data first if the variance matters.

<small>Defined in <code>drift/welford.go</code>.</small>

### CUSUM.Add

```go
func (c *CUSUM) Add(x float64) Signal
```

Add folds x into both sums and reports Up when S⁺ > h, Down when S⁻ > h, and
when both are over the larger one, Up on a tie. It does not allocate.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.Lower

```go
func (c *CUSUM) Lower() float64
```

Lower is S⁻, the evidence for a downward shift. Never negative.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.N

```go
func (c *CUSUM) N() int64
```

N is the number of samples folded in.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.Reset

```go
func (c *CUSUM) Reset()
```

Reset sets both sums to zero, keeping target, k and h, and clears N and
Skipped.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.Since

```go
func (c *CUSUM) Since() int64
```

Since is the number of consecutive samples, ending with the latest, for
which the leading side has been above zero: an estimate of how many samples
ago the shift began, so it started at sample N() − Since() + 1.

The leading side is the larger of S⁺ and S⁻, Up on a tie. That is always the
side Add just signalled, if it signalled: a side over h is larger than one
that is not, and when both are over, Add picks the larger too. Since is 0
while both sides are at zero.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.Skipped

```go
func (c *CUSUM) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>drift/cusum.go</code>.</small>

### CUSUM.Upper

```go
func (c *CUSUM) Upper() float64
```

Upper is S⁺, the evidence for an upward shift. Never negative.

<small>Defined in <code>drift/cusum.go</code>.</small>

### Decayed.Reset

```go
func (d *Decayed) Reset()
```

Reset forgets the baseline, the average and both counts, keeping the window.

<small>Defined in <code>drift/decayed.go</code>.</small>

### Decayed.Resets

```go
func (d *Decayed) Resets() int64
```

Resets is the number of readings below their predecessor, taken as counter
resets.

<small>Defined in <code>drift/decayed.go</code>.</small>

### Decayed.Skipped

```go
func (d *Decayed) Skipped() int64
```

Skipped is the number of readings not after their predecessor.

<small>Defined in <code>drift/decayed.go</code>.</small>

### Decayed.Update

```go
func (d *Decayed) Update(total uint64, at time.Time) (rate float64, ok bool)
```

Update reports the counter's total as read at time at, and returns the
average rate in total-per-second with ok = true when the reading produced a
rate. It returns the current Value (NaN if none yet) with ok = false for the
first reading, for a reading not after the previous one, and for a counter
reset. It does not allocate.

<small>Defined in <code>drift/decayed.go</code>.</small>

### Decayed.Value

```go
func (d *Decayed) Value() float64
```

Value is the current average rate in total-per-second, NaN before the first
rate.

<small>Defined in <code>drift/decayed.go</code>.</small>

### Decayed.Window

```go
func (d *Decayed) Window() time.Duration
```

Window is the time constant.

<small>Defined in <code>drift/decayed.go</code>.</small>

### DefaultCUSUM

```go
func DefaultCUSUM(target, sigma float64) (CUSUM, error)
```

DefaultCUSUM returns NewCUSUM(target, 0.5·sigma, 5·sigma) for a series with
in-control standard deviation sigma > 0.

Montgomery recommends k = σ/2, which tunes the test for a one-sigma shift,
with h = 4σ or h = 5σ. Two-sided, these give an in-control average run
length of about 168 samples at h = 4 and about 465 at h = 5, against about
8.4 and 10.4 samples to detect a one-sigma shift. 5 is the default because
it roughly triples the time between false alarms for two extra samples of
delay; on a series read every second, h = 4 would raise a false alarm about
every three minutes per series. TestCUSUMARL measures both run lengths.

<small>Defined in <code>drift/cusum.go</code>.</small>

### DefaultEWMAChart

```go
func DefaultEWMAChart(target, sigma float64) (EWMAChart, error)
```

DefaultEWMAChart returns NewEWMAChart(target, sigma, 0.2, 3).

These are the literature's working values, not a tuning for your data.
Montgomery recommends λ between 0.05 and 0.25, and reports that L = 3, the
usual three-sigma width, works well, particularly with the larger λ. Lucas
and Saccucci's tables put the limit for an in-control average run length of
500 at λ = 0.2 slightly inside 3 (L ≈ 2.96 for the asymptotic band); 3 is
kept because it is the convention readers recognise and it errs towards
fewer false alarms. Measured here with the exact, time-varying band, the
in-control average run length at these values is about 560 samples, and a
one-sigma shift is detected in about 10 (TestEWMAChartARL).

<small>Defined in <code>drift/chart.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("drift: invalid parameter")
```

ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
infinite, or outside the range its doc names. Match it with errors.Is; the
message text is not part of the API.

<small>Defined in <code>drift/signal.go</code>.</small>

### EWMA.Add

```go
func (e *EWMA) Add(x float64)
```

Add folds x into the average. It does not allocate.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMA.Alpha

```go
func (e *EWMA) Alpha() float64
```

Alpha is the smoothing factor.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMA.N

```go
func (e *EWMA) N() int64
```

N is the number of values folded in.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMA.Reset

```go
func (e *EWMA) Reset()
```

Reset forgets every value and Skipped, keeping alpha.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMA.Skipped

```go
func (e *EWMA) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMA.Value

```go
func (e *EWMA) Value() float64
```

Value is the current average, NaN before the first value.

<small>Defined in <code>drift/ewma.go</code>.</small>

### EWMAChart.Add

```go
func (c *EWMAChart) Add(x float64) Signal
```

Add folds x into the statistic and reports whether it is now outside the
limits for this sample: Up above, Down below, None inside. A signal does
not reset anything; the chart keeps running, and will usually keep
signalling while the shift lasts, until you call Reset. It does not
allocate.

<small>Defined in <code>drift/chart.go</code>.</small>

### EWMAChart.Limits

```go
func (c *EWMAChart) Limits() (lo, hi float64)
```

Limits is the band for the current sample count t. At t = 0 it is the
single point target, since no sample has been judged yet.

<small>Defined in <code>drift/chart.go</code>.</small>

### EWMAChart.N

```go
func (c *EWMAChart) N() int64
```

N is the number of samples judged.

<small>Defined in <code>drift/chart.go</code>.</small>

### EWMAChart.Reset

```go
func (c *EWMAChart) Reset()
```

Reset restarts the chart at z = target and t = 0, keeping its parameters
and clearing Skipped.

<small>Defined in <code>drift/chart.go</code>.</small>

### EWMAChart.Skipped

```go
func (c *EWMAChart) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>drift/chart.go</code>.</small>

### EWMAChart.Statistic

```go
func (c *EWMAChart) Statistic() float64
```

Statistic is z_t, the smoothed series; target before the first sample.

<small>Defined in <code>drift/chart.go</code>.</small>

### NewCUSUM

```go
func NewCUSUM(target, k, h float64) (CUSUM, error)
```

NewCUSUM returns a two-sided CUSUM around target with slack k >= 0 and
decision interval h > 0, all finite and in the data's units.

<small>Defined in <code>drift/cusum.go</code>.</small>

### NewDecayed

```go
func NewDecayed(window time.Duration) (Decayed, error)
```

NewDecayed returns an averager with time constant window > 0: the time over
which a step in the rate is 63% absorbed.

<small>Defined in <code>drift/decayed.go</code>.</small>

### NewEWMA

```go
func NewEWMA(alpha float64) (EWMA, error)
```

NewEWMA returns an average with smoothing factor alpha in (0, 1]. alpha = 1
tracks the latest value exactly; small alpha averages over about 1/alpha
samples.

<small>Defined in <code>drift/ewma.go</code>.</small>

### NewEWMAChart

```go
func NewEWMAChart(target, sigma, lambda, L float64) (EWMAChart, error)
```

NewEWMAChart returns a chart around target for a series with in-control
standard deviation sigma > 0, smoothing lambda in (0, 1] and limit width L
> 0 in units of the statistic's own standard deviation. All must be finite.

lambda = 1 makes z_t the latest sample, and the chart becomes a plain
Shewhart chart with limits target ± Lσ.

<small>Defined in <code>drift/chart.go</code>.</small>

### NewEWMAHalfLife

```go
func NewEWMAHalfLife(samples float64) (EWMA, error)
```

NewEWMAHalfLife returns an average in which a value's weight halves every
`samples` samples: alpha = 1 − 2^(−1/samples). samples must be finite and
positive; it need not be a whole number.

<small>Defined in <code>drift/ewma.go</code>.</small>

### NewPageHinkley

```go
func NewPageHinkley(delta, lambda float64) (PageHinkley, error)
```

NewPageHinkley returns a two-sided Page–Hinkley test with tolerance delta
>= 0 and threshold lambda > 0, both finite.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### PageHinkley.Add

```go
func (p *PageHinkley) Add(x float64) Signal
```

Add folds x in and reports whether either side is past lambda. Like CUSUM,
a signal changes nothing: the statistics keep accumulating until Reset. It
does not allocate.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### PageHinkley.N

```go
func (p *PageHinkley) N() int64
```

N is the number of samples folded in.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### PageHinkley.Reset

```go
func (p *PageHinkley) Reset()
```

Reset forgets the mean and both sums, keeping delta and lambda, and clears
N and Skipped.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### PageHinkley.Skipped

```go
func (p *PageHinkley) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### PageHinkley.Statistic

```go
func (p *PageHinkley) Statistic() (up, down float64)
```

Statistic is the evidence for each direction, m⁺ − M⁺ and M⁻ − m⁻. Both
are never negative, and a side signals when it exceeds lambda.

<small>Defined in <code>drift/pagehinkley.go</code>.</small>

### Signal.String

```go
func (s Signal) String() string
```

String returns "none", "up" or "down", and "Signal(n)" for anything else.

<small>Defined in <code>drift/signal.go</code>.</small>

### Welford.Add

```go
func (w *Welford) Add(x float64)
```

Add folds x into the running statistics. It does not allocate.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.Mean

```go
func (w *Welford) Mean() float64
```

Mean is the arithmetic mean, NaN when nothing has been added.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.Merge

```go
func (w *Welford) Merge(o Welford)
```

Merge folds o into w, as if every value added to o had been added to w,
skipped ones included. The order the values arrived in is lost, which is
fine for a mean and a variance and is why per-shard accumulators can be
combined at the end. The result equals one pass over all the values up to
rounding, not bit for bit.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.N

```go
func (w *Welford) N() int64
```

N is the number of values folded in.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.PopVariance

```go
func (w *Welford) PopVariance() float64
```

PopVariance is the population variance, with the n divisor, NaN when
nothing has been added. Use it when the values are the whole population
rather than a sample of one.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.Reset

```go
func (w *Welford) Reset()
```

Reset empties the accumulator, Skipped included.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.Skipped

```go
func (w *Welford) Skipped() int64
```

Skipped is the number of non-finite values Add refused.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.StdDev

```go
func (w *Welford) StdDev() float64
```

StdDev is the square root of Variance, NaN below two values.

<small>Defined in <code>drift/welford.go</code>.</small>

### Welford.Variance

```go
func (w *Welford) Variance() float64
```

Variance is the sample variance, with the n−1 divisor, NaN below two values.

<small>Defined in <code>drift/welford.go</code>.</small>
