---
title: "runi/season"
weight: 3
---

```go
import "github.com/org-runink/runi/season"
```

Package season finds the repeating pattern in a series and the points where
its trend breaks, and splits the series into trend, season and residual.

It is the classical additive decomposition

y(t) = trend(t) + season(t) + residual(t)

with a piecewise-linear trend and a Fourier-series season. Each piece is
usable on its own: Period answers "does this repeat, and how often?",
FitFourier models one known season, Changepoints finds trend breaks, and
Decompose runs all three together and can extend the result forward.

Classical is the second, cheaper entry point, for when the period is already
known: the textbook moving-average decomposition, the operation
statsmodels.tsa.seasonal.seasonal_decompose performs, computed in one pass.
No period detection, no trend breaks, no forecast, and about forty times
less work than Decompose.

This is not Prophet. Prophet is Meta's forecasting library; it fits a
related model with a Bayesian treatment of changepoints, holidays and
several seasonalities at once. If you want Prophet, use Prophet. This
package is the textbook decomposition underneath, with no dependencies.

Isolated outliers are filtered out of what the estimators measure, but not
out of what they return. Every estimate here is a sum of squares, so one bad
reading of magnitude a contributes a² and outvotes the rest of the series:
unguarded, a single spike is enough to hide a weekly season completely and
to be reported as two structural breaks around itself. So Period,
Changepoints and Decompose score on a copy with isolated spikes replaced by
their local median, while the trend, season and residual they report are
fitted to the series as given. A bad day therefore still shows up as a large
Residual, where a caller looking for anomalies can find it, instead of being
smoothed away. A shift in level that LASTS is a break, not an outlier, and
survives the filter. Classical is the one exception, and says so in its own
doc: there the moving average IS the estimate, and filtering it would make
the result disagree with every other implementation of the classical
decomposition.

Limits, stated so they are not discovered later:
  - One seasonality. A series with both a weekly and a yearly cycle gets the
    strongest one only.
  - The trend is least-squares lines between changepoints, fitted
    independently: it may jump at a changepoint (a level shift is a break),
    and a forecast extends the last line.
  - Short series are refused rather than guessed at: Period needs
    MinPeriodLength (28) points and two full cycles, Decompose needs 4
    points, Classical needs two full cycles of the period it is given. 28 is
    not arbitrary — it is the shortest series in which a cycle
    can in fact be found, because the bar a lag must clear rises as the
    series shortens while the autocorrelation estimator's ceiling falls, and
    below 28 the bar sits above the ceiling.
  - A spike only a few times the noise is indistinguishable from the noise,
    and is neither filtered nor reliably detected. The filter earns its keep
    on spikes well clear of the noise, which is where the damage was.
  - No uncertainty intervals.

# Measured against statsmodels

Classical computes what statsmodels.tsa.seasonal.seasonal_decompose
computes, by the same method, and it is FASTER: n=4,000 at period 24 takes
0.0213 ms here against 0.1815 ms there, 8.5x faster, with the two agreeing to
2e-13 on a series of magnitude 300 — a few ulps of float64, which is to say
they produce the same numbers. That is the like-for-like row, and it is the
one to quote.

Decompose used to be 5x SLOWER than seasonal_decompose. It is now slightly
faster — 0.155 ms against 0.1815 ms with the same period and the trend-break
search off — but it is still doing a different and larger job, solving two
least-squares systems where a moving average takes two additions a point. It
is the wrong tool for a decomposition whose period you already know — that is
what Classical is for — and the right one for the three things a moving
average cannot do at all. Each piece costs what it costs:

Classical, period given             0.0213 ms
Decompose, period given, no breaks  0.155 ms
plus the BIC changepoint search     16.9 ms   (the default)
plus detecting the period too       53.5 ms
Period detection on its own         3.54 ms

So: a moving average cannot tell you the period, cannot tell you where the
trend broke, and cannot extrapolate. Those three are what the extra time
buys, and when you need none of them, Classical is the function.

Measured on an ASUS Ascent GX10, 20 cores, aarch64, Go 1.27.2, statsmodels
0.15.0 on Python 3.12.3.

## Index

- `type` [Decomposition](#decomposition)
- `type` [Fourier](#fourier)
- `type` [Options](#options)
- `func` [Changepoints](#changepoints)
- `func` [Classical](#classical)
- `func` [Decompose](#decompose)
- `method` [Decomposition.Forecast](#decompositionforecast)
- `const` [ErrInvalid](#errinvalid)
- `const` [ErrTooShort](#errtooshort)
- `func` [FitFourier](#fitfourier)
- `method` [Fourier.Amplitude](#fourieramplitude)
- `method` [Fourier.At](#fourierat)
- `const` [MinPeriodLength](#minperiodlength)
- `func` [Period](#period)

### Decomposition

```go
type Decomposition struct
```

Decomposition is a series split into trend, season and residual.

<small>Defined in <code>season/season.go</code>.</small>

### Fourier

```go
type Fourier struct
```

Fourier is a fitted seasonal pattern: Mean plus, for each harmonic k,
A[k]·cos(2π(k+1)t/Period) + B[k]·sin(2π(k+1)t/Period).

<small>Defined in <code>season/season.go</code>.</small>

### Options

```go
type Options struct
```

Options configure Decompose.

<small>Defined in <code>season/season.go</code>.</small>

### Changepoints

```go
func Changepoints(x []float64, maxK int) []int
```

Changepoints returns up to maxK indices where the trend of x breaks, in
ascending order.

It splits greedily, each time where one straight line becomes two with the
largest drop in squared error. A split is kept only if that drop exceeds
3·σ²·ln(n), a BIC-style price for the two extra parameters and the break's
position, with the noise level σ estimated robustly from the median absolute
deviation of first differences. Without that price every split of a noisy
line looks like an improvement, and breaks are invented in smooth data.
Segments are at least minSegment points long. NaN values are forward-filled.

<small>Defined in <code>season/season.go</code>.</small>

### Classical

```go
func Classical(x []float64, period int) (*Decomposition, error)
```

Classical splits x into trend, season and residual by the classical
moving-average method, in one pass over the series.

It is the textbook decomposition, and it is exactly what
statsmodels.tsa.seasonal.seasonal_decompose(x, period=period,
model="additive") computes: a centred moving average over one period is the
trend, the detrended series averaged by phase is the season, and the season
is then centred on zero so that it carries no level. For an EVEN period the
average is taken over period+1 points with half weight at each end, because
a window of 24 hourly points has no centre and a plain average of it would
be assigned half an hour off; for an odd period it is the plain average of
period points. The two implementations agree to 2e-13 on the interior of
series of magnitude 300 — a few ulps of float64, the only disagreement left
being the order the additions happen in — checked against statsmodels 0.15.0
over six series by benchmarks/verify_classical.py.

It is O(n) — a running window sum, then one pass to average by phase — where
Decompose solves two least-squares systems: n=4,000 at period 24 takes
0.0213 ms against Decompose's 0.155 ms for the same arguments, and 0.1815 ms
for seasonal_decompose. It is the fast path when the period is already known.

What it does NOT do, and what Decompose is for:
  - No period detection. The period you pass is the period used; a wrong one
    is fitted without complaint. Period answers "does this repeat, and how
    often?", and Decompose runs it for you.
  - No trend breaks. The returned Changepoints is always nil. A level shift
    is smeared across one window of the moving average rather than reported.
  - No forecast. The result carries no trend model, so Forecast returns nil:
    a moving average says nothing about the point after the last one.
  - No outlier filtering. The average IS the estimate here, so a spike lands
    in the trend for the period around it and in the season for its phase.
    Decompose scores on a Hampel-cleaned copy; this does not, because the
    classical decomposition is defined without it and filtering would make
    the result disagree with every other implementation of it.

# The ends

A centred average of period points is undefined for the first and last
period/2 points, because there is no window to average: that is a property
of the method, not of this implementation. statsmodels returns NaN there.
This returns the nearest defined average instead — the leading period/2
points all hold the first real average, the trailing period/2 all hold the
last — because a NaN in a component poisons the sum, the plot and the
variance a caller computes from it, while a held value is a stated
approximation.

Two consequences, both deliberate. The season is averaged over the interior
ONLY, so those held points do not pull it towards the series' own curvature,
which is what keeps the figures identical to statsmodels. And the residual
is x − trend − season at every index, so the three components add up to the
series everywhere including the ends: the held trend's error lands in the
residual, visible, rather than being hidden in a smoothed trend. Those first
and last period/2 residuals are therefore not comparable with the interior
ones — they carry up to half a period of trend curvature. Treat the interior
[period/2, len(x)−period/2) as the decomposition and the ends as padding.

NaN values are forward-filled and leading NaNs take the first real value, as
everywhere else in this package. A period below 2 is an error, and so is a
series shorter than two full cycles (ErrTooShort): with less than that, some
phase of the season has no point at all to average.

<small>Defined in <code>season/season.go</code>.</small>

### Decompose

```go
func Decompose(x []float64, opt Options) (*Decomposition, error)
```

Decompose splits x into trend + season + residual.

The order matters and is fixed: the period is found on the differenced
series; a season is fitted after removing a straight line whose slope is the
median rise over one full period (a seasonal Theil–Sen line), so neither the
season nor a spike can tilt it; changepoints are found after removing that
season (so the season is not mistaken for breaks); then the trend is refitted
between them and the season refitted once on what the trend leaves. Both
season fits clean outliers by comparing each point with the same phase in
neighbouring cycles rather than with its neighbours in time, so a spike is
removed and a sharp feature that recurs every cycle is kept. NaN values are forward-filled, and
leading NaNs take the first real value.

<small>Defined in <code>season/season.go</code>.</small>

### Decomposition.Forecast

```go
func (d *Decomposition) Forecast(h int) []float64
```

Forecast extends the decomposition h steps past the end of the series: the
last trend line continued, plus the season.

<small>Defined in <code>season/season.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("season: invalid argument")
```

ErrInvalid is wrapped by every argument error: a period or harmonic count
out of range, a series too short to model (ErrTooShort), a series with no
values, or a series that cannot identify the requested period. Match it with
errors.Is; the message text is not part of the API.

<small>Defined in <code>season/season.go</code>.</small>

### ErrTooShort

```go
var ErrTooShort = invalid("season: series too short")
```

ErrTooShort is returned when a series has too few points to model. It wraps
ErrInvalid.

<small>Defined in <code>season/season.go</code>.</small>

### FitFourier

```go
func FitFourier(x []float64, period, harmonics int) (Fourier, error)
```

FitFourier fits a Fourier series of the given period to x by least squares.

Least squares, not a projection: the projection shortcut is exact only when
the series holds a whole number of cycles and biased otherwise. harmonics is
capped at period/2, the most a period can carry. NaN values are
forward-filled.

<small>Defined in <code>season/season.go</code>.</small>

### Fourier.Amplitude

```go
func (f Fourier) Amplitude(k int) float64
```

Amplitude returns the amplitude of harmonic k (1 is the base cycle), or 0
when the fit has no such harmonic.

<small>Defined in <code>season/season.go</code>.</small>

### Fourier.At

```go
func (f Fourier) At(t int) float64
```

At returns the seasonal effect at time t, excluding Mean.

t is taken modulo the period first. That is what a season MEANS, so it
changes no answer mathematically, but it does change the arithmetic: the
angle handed to Cos and Sin is now under 2π·harmonics instead of growing
with t, and a sine of a large angle is not as accurate as a sine of a small
one. By t=4,000 on a 24-point cycle the angle is past a thousand radians,
where one ulp of the angle is already 2e-13 of a radian and the rounding in
forming it has nowhere to hide. Reducing first makes At agree with the
basis FitFourier actually fitted — which is tabulated per phase, and so has
always used the small angle — rather than drifting from it down the series.

<small>Defined in <code>season/season.go</code>.</small>

### MinPeriodLength

```go
const MinPeriodLength = 28
```

MinPeriodLength is the shortest series Period will look for a cycle in.

28, because that is the shortest series in which a cycle can actually be
found, and a lower figure would be a promise the function cannot keep. The
autocorrelation bar a lag must clear (see Period) rises as the series
shortens, while the most the estimator can report at lag L falls to about
1 − L/m.
Below 28 points the bar is above the ceiling: no series of any shape clears
it, so Period returns 0 whatever it is given. Callers passing 8 to 27 points
got that same 0 before this constant said so; what changes is that the
package no longer advertises a floor it cannot answer above.

<small>Defined in <code>season/season.go</code>.</small>

### Period

```go
func Period(x []float64, maxPeriod int) int
```

Period returns the dominant seasonal period of x, or 0 when there is none.

It looks for the highest PEAK in the autocorrelation of the
once-differenced series (differencing removes a linear trend, which would
otherwise correlate with itself at every lag). A lag counts only if its
autocorrelation clears a bar that rises as the series shortens and is raised
for the number of lags tried, so white noise yields a season under 1% of the
time, and only if at least two full cycles fit in the series. The bar is
never below 0.2. The winner is then checked against
its neighbours by seasonal fit, which settles off-by-one peaks. maxPeriod
caps the search; 0 means half the series. NaN values are forward-filled.

<small>Defined in <code>season/season.go</code>.</small>
