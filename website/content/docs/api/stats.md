---
title: "runi/stats"
weight: 1
---

```go
import "github.com/org-runink/runi/stats"
```

Package stats is the first ten minutes of a data problem: look at the
numbers, split them, scale them, and check whether two columns move together.

It exists because Go's standard library has none of this, and a data
scientist arriving from pandas or scikit-learn hits that wall immediately.
Nothing here is novel; all of it is textbook. The value is that it is
correct, documented about its edges, and has no dependencies.

# The one opinion

Scalers are FITTED and then APPLIED. [Standardiser] and [MinMaxScaler] are
computed from training data and then applied, unchanged, to validation and
test data:

sc := stats.FitStandardiser(train)
trainZ, err := sc.Transform(train)
if err != nil {
	return err // ErrNotFitted: train was empty
}
testZ, err := sc.Transform(test) // the TRAINING mean and sd, deliberately

Re-fitting on the test set leaks its distribution into the model and makes
every score that follows too optimistic. That mistake is easy to make and
impossible to see in the output, so the API makes the correct thing the
obvious thing: there is no function that scales a slice in one step.

# NaN

Functions here do NOT silently skip NaN. A NaN in the input produces a NaN in
the output, because quietly dropping values changes the denominator and the
caller is rarely told. Use [DropNaN] when that is what you mean.

# Is it real?

A correlation or a slope is an effect size: how strong a relationship looks,
not how likely it is to be there at all. Two independent random series of
four points clear |r| >= 0.5 about half the time by luck; at twenty points
it is about 2.5%. So the effect sizes here come with their evidence:

c, ok := stats.Correlate(a, b)                  // r, n and the two-sided p-value
t := stats.Trend(y)                             // OLS slope, standard error, t and p
q, keep := stats.BenjaminiHochberg(ps, 0.05)    // across a whole family of tests

[Correlate] refuses (ok=false) below [MinCorrelationSamples] aligned points,
where a bare r misleads more than it informs. Testing many pairs at once
compounds a per-test 5% error rate, so [Adjust] applies Benjamini–Hochberg,
which controls the expected share of false findings among those kept. Every
"not enough evidence" case returns p = 1, never 0 or NaN, so a caller that
forgets to check still does the conservative thing.

These are tests against zero (no correlation, no slope) under the usual
Pearson and OLS assumptions, including independent observations. Two series
that both trend will correlate whether or not they are related: detrend
before you Correlate them. Nothing here tests causation. The Student-t tail
comes from the regularised incomplete beta function (the standard library
has no t-distribution), pinned by the tests to published critical values
and to reference values within a relative error of 1e-9.

## Index

- `type` [Correlation](#correlation)
- `type` [Fold](#fold)
- `type` [MinMaxScaler](#minmaxscaler)
- `type` [Standardiser](#standardiser)
- `type` [Summary](#summary)
- `type` [TrendTest](#trendtest)
- `func` [Adjust](#adjust)
- `func` [BenjaminiHochberg](#benjaminihochberg)
- `func` [Correlate](#correlate)
- `func` [CorrelationPValue](#correlationpvalue)
- `const` [DefaultAlpha](#defaultalpha)
- `func` [Describe](#describe)
- `func` [DropNaN](#dropnan)
- `const` [ErrEmpty](#errempty)
- `const` [ErrInvalid](#errinvalid)
- `const` [ErrLengthMismatch](#errlengthmismatch)
- `const` [ErrNotFitted](#errnotfitted)
- `func` [FitMinMax](#fitminmax)
- `func` [FitStandardiser](#fitstandardiser)
- `func` [IQR](#iqr)
- `func` [Max](#max)
- `func` [Mean](#mean)
- `func` [Median](#median)
- `func` [Min](#min)
- `const` [MinCorrelationSamples](#mincorrelationsamples)
- `method` [MinMaxScaler.Inverse](#minmaxscalerinverse)
- `method` [MinMaxScaler.Transform](#minmaxscalertransform)
- `func` [NewMinMaxScaler](#newminmaxscaler)
- `func` [NewStandardiser](#newstandardiser)
- `func` [Pearson](#pearson)
- `func` [PopStdDev](#popstddev)
- `func` [PopVariance](#popvariance)
- `func` [Quantile](#quantile)
- `func` [Quantiles](#quantiles)
- `func` [RollingFolds](#rollingfolds)
- `func` [Slope](#slope)
- `func` [Spearman](#spearman)
- `func` [Split](#split)
- `func` [SplitIndex](#splitindex)
- `method` [Standardiser.Inverse](#standardiserinverse)
- `method` [Standardiser.Transform](#standardisertransform)
- `func` [StdDev](#stddev)
- `func` [Trend](#trend)
- `method` [TrendTest.Falling](#trendtestfalling)
- `method` [TrendTest.Rising](#trendtestrising)
- `func` [TwoSidedP](#twosidedp)
- `func` [Variance](#variance)

### Correlation

```go
type Correlation struct
```

Correlation is a Pearson coefficient together with the evidence that makes
it a finding rather than a coincidence.

<small>Defined in <code>stats/significance.go</code>.</small>

### Fold

```go
type Fold struct
```

Fold is one split of a cross-validation: the index ranges to train on and to
validate against.

<small>Defined in <code>stats/prep.go</code>.</small>

### MinMaxScaler

```go
type MinMaxScaler struct
```

MinMaxScaler rescales a column onto [0,1] by its observed range.

It is the right choice when a bounded input is required and the wrong choice
when outliers matter: one extreme value compresses everything else toward a
single point. [Standardiser] is more robust to that; neither is robust to a
value outside the fitted range, which Transform will map outside [0,1]
rather than clip. Clipping silently would hide that the new data does not
look like the training data, which is usually the most useful thing to know.

As with [Standardiser], the fields are exported so a fitted scaler can be
stored; reload one with [NewMinMaxScaler]. A struct literal or one filled in
by json.Unmarshal is unfitted and its methods return [ErrNotFitted]. The
fields alone cannot say whether a scaler was fitted, because fitting a column
of zeros gives Min = Max = 0, which is also the zero value.

<small>Defined in <code>stats/prep.go</code>.</small>

### Standardiser

```go
type Standardiser struct
```

Standardiser rescales a column to zero mean and unit standard deviation,
z = (x − mean) / sd.

Fit it on training data and apply it, unchanged, everywhere else. Its fields
are exported so a fitted scaler can be stored alongside a model — a model is
only valid for the scaling it was trained under. To reload one, pass the
stored fields to [NewStandardiser]: a Standardiser built any other way,
including a struct literal or one filled in by json.Unmarshal, is unfitted
and its methods return [ErrNotFitted].

<small>Defined in <code>stats/prep.go</code>.</small>

### Summary

```go
type Summary struct
```

Summary is what [Describe] reports: the shape of a column at a glance.

<small>Defined in <code>stats/stats.go</code>.</small>

### TrendTest

```go
type TrendTest struct
```

TrendTest is an ordinary-least-squares slope of a series against its index,
with the test of that slope against zero.

<small>Defined in <code>stats/significance.go</code>.</small>

### Adjust

```go
func Adjust(cs []Correlation, alpha float64)
```

Adjust applies Benjamini–Hochberg at alpha across every correlation in cs,
setting each Q and Significant in place. Use it when one analysis tests many
pairs: k series give k(k-1)/2 tests, and a per-test 5% error rate compounds.

<small>Defined in <code>stats/significance.go</code>.</small>

### BenjaminiHochberg

```go
func BenjaminiHochberg(p []float64, alpha float64) (q []float64, reject []bool)
```

BenjaminiHochberg controls the false discovery rate across a family of m
tests at level alpha. It returns, aligned with p, the adjusted q-values and
whether each hypothesis is rejected (q <= alpha).

The q-values use the step-up form q(i) = min over j >= i of (m/j)·p(j),
clamped to 1, which keeps them monotone in p. An alpha outside (0, 1] is
replaced by DefaultAlpha.

<small>Defined in <code>stats/significance.go</code>.</small>

### Correlate

```go
func Correlate(a, b []float64) (c Correlation, ok bool)
```

Correlate returns the Pearson correlation of a and b over their overlapping
leading window (the first min(len(a), len(b)) values), with its sample size
and two-sided p-value.

ok is false when the overlap is below MinCorrelationSamples, or when r is
undefined because either series is constant over the window. A caller that
ignores ok gets a zero Correlation, which is not significant.

<small>Defined in <code>stats/significance.go</code>.</small>

### CorrelationPValue

```go
func CorrelationPValue(r float64, n int) float64
```

CorrelationPValue is the two-sided p-value for a Pearson r on n paired
observations, under the null that the true correlation is zero:

t = r·sqrt(n-2)/sqrt(1-r²),  df = n-2

It returns 1 (no evidence) when n < 3 or r is NaN, and 0 for |r| = 1.

<small>Defined in <code>stats/significance.go</code>.</small>

### DefaultAlpha

```go
const DefaultAlpha = 0.05
```

DefaultAlpha is the conventional significance level and false discovery
rate: 0.05.

<small>Defined in <code>stats/significance.go</code>.</small>

### Describe

```go
func Describe(x []float64) Summary
```

Describe summarises a column, the way you would look at it first.

Unlike the rest of this package, Describe DOES exclude NaN — and tells you
how many it excluded in [Summary.NaN], so the omission is visible rather than
silent. N counts the values actually used.

<small>Defined in <code>stats/stats.go</code>.</small>

### DropNaN

```go
func DropNaN(x []float64) (clean []float64, removed int)
```

DropNaN returns x without its NaN values, and how many were removed. The
input is not modified.

<small>Defined in <code>stats/stats.go</code>.</small>

### ErrEmpty

```go
var ErrEmpty = errors.New("stats: no data")
```

ErrEmpty is returned when a computation has no data to work with.

<small>Defined in <code>stats/stats.go</code>.</small>

### ErrInvalid

```go
var ErrInvalid = errors.New("stats: invalid argument")
```

ErrInvalid is wrapped by every constructor error: a parameter that is NaN,
infinite, or outside the range its doc names. Match it with errors.Is; the
message text is not part of the API.

<small>Defined in <code>stats/prep.go</code>.</small>

### ErrLengthMismatch

```go
var ErrLengthMismatch = errors.New("stats: samples have different lengths")
```

ErrLengthMismatch is returned when two samples must be the same length.

<small>Defined in <code>stats/stats.go</code>.</small>

### ErrNotFitted

```go
var ErrNotFitted = errors.New("stats: scaler has not been fitted")
```

ErrNotFitted is returned by a scaler that has not been fitted to any data:
the zero value, a struct literal, or one decoded from storage without going
through [NewStandardiser] or [NewMinMaxScaler].

<small>Defined in <code>stats/prep.go</code>.</small>

### FitMinMax

```go
func FitMinMax(x []float64) *MinMaxScaler
```

FitMinMax computes the range of x. A constant column maps to zero. An empty x
gives an unfitted scaler.

<small>Defined in <code>stats/prep.go</code>.</small>

### FitStandardiser

```go
func FitStandardiser(x []float64) *Standardiser
```

FitStandardiser computes the mean and sample standard deviation of x.

A constant column has zero spread and cannot be standardised; its scaler
leaves values unchanged rather than dividing by zero, so a constant feature
becomes a column of zeros after centring instead of a column of NaNs that
poisons everything downstream.

An empty x gives an unfitted scaler.

<small>Defined in <code>stats/prep.go</code>.</small>

### IQR

```go
func IQR(x []float64) float64
```

IQR is the interquartile range, Q3 − Q1: a spread measure that a few extreme
values cannot move, unlike the standard deviation.

<small>Defined in <code>stats/stats.go</code>.</small>

### Max

```go
func Max(x []float64) float64
```

Max returns the largest value, or NaN for an empty slice.

<small>Defined in <code>stats/stats.go</code>.</small>

### Mean

```go
func Mean(x []float64) float64
```

Mean is the arithmetic mean. It returns NaN for an empty slice, since the
mean of nothing is not zero.

<small>Defined in <code>stats/stats.go</code>.</small>

### Median

```go
func Median(x []float64) float64
```

Median is the 0.5 quantile.

<small>Defined in <code>stats/stats.go</code>.</small>

### Min

```go
func Min(x []float64) float64
```

Min and Max return the smallest and largest value. Both return NaN for an
empty slice, and propagate a NaN present in the input.

<small>Defined in <code>stats/stats.go</code>.</small>

### MinCorrelationSamples

```go
const MinCorrelationSamples = 20
```

MinCorrelationSamples is the number of aligned observations Correlate needs
before it reports anything.

Twenty is where a bare |r| >= 0.5 rule's false-positive rate on independent
noise falls to about the nominal two-sided 5% (it is roughly 50% at n = 4,
31% at n = 6 and 20% at n = 8). With df = 18 the t-test still has useful power:
a true |ρ| near 0.6 is detected at α = 0.05 about 80% of the time. Lowering
it does not find more relationships; it invents them.

<small>Defined in <code>stats/significance.go</code>.</small>

### MinMaxScaler.Inverse

```go
func (s *MinMaxScaler) Inverse(z []float64) ([]float64, error)
```

Inverse maps scaled values back to the original units.

<small>Defined in <code>stats/prep.go</code>.</small>

### MinMaxScaler.Transform

```go
func (s *MinMaxScaler) Transform(x []float64) ([]float64, error)
```

Transform maps x onto [0,1] using the fitted range. Values outside the
fitted range map outside [0,1]; see the type's documentation.

<small>Defined in <code>stats/prep.go</code>.</small>

### NewMinMaxScaler

```go
func NewMinMaxScaler(min, max float64) (MinMaxScaler, error)
```

NewMinMaxScaler rebuilds a fitted MinMaxScaler from a stored Min and Max,
for example ones saved with a model and decoded with encoding/json.

Both must be finite and min must not exceed max, which holds for every scaler
[FitMinMax] fits to finite data. min == max is allowed: it is how a constant
column is stored, and it maps every value to zero. Anything else returns an
error wrapping [ErrInvalid].

<small>Defined in <code>stats/prep.go</code>.</small>

### NewStandardiser

```go
func NewStandardiser(mean, sd float64) (Standardiser, error)
```

NewStandardiser rebuilds a fitted Standardiser from a stored Mean and StdDev,
for example ones saved with a model and decoded with encoding/json.

mean must be finite and sd finite and positive, which holds for every scaler
[FitStandardiser] fits to finite data: a constant column is stored with sd 1,
never 0. Anything else returns an error wrapping [ErrInvalid].

<small>Defined in <code>stats/prep.go</code>.</small>

### Pearson

```go
func Pearson(x, y []float64) (float64, error)
```

Pearson is the linear correlation coefficient between x and y, in [-1,1].

It measures LINEAR association only: a perfect parabola through the origin
has a Pearson correlation near zero. Reach for [Spearman] when the
relationship may be monotonic but not straight.

<small>Defined in <code>stats/stats.go</code>.</small>

### PopStdDev

```go
func PopStdDev(x []float64) float64
```

PopStdDev is the population standard deviation.

<small>Defined in <code>stats/stats.go</code>.</small>

### PopVariance

```go
func PopVariance(x []float64) float64
```

PopVariance is the population variance, dividing by n.

<small>Defined in <code>stats/stats.go</code>.</small>

### Quantile

```go
func Quantile(x []float64, q float64) float64
```

Quantile returns the q-th quantile of x for q in [0,1], using linear
interpolation between order statistics — the same definition as NumPy's
default and R's type 7, so a number computed here matches one computed there.

It does not modify x: the data is copied before sorting.

Asking for several quantiles of the same column calls [Quantiles] instead:
each call here sorts its own copy, so three quantiles of a 200,000-point
column is three sorts of 200,000 points.

<small>Defined in <code>stats/stats.go</code>.</small>

### Quantiles

```go
func Quantiles(x []float64, qs ...float64) []float64
```

Quantiles returns several quantiles of x from ONE sorted copy, in the order
asked for. It is the function to reach for when summarising a column, which
is the common case: a median and a pair of tails is three calls to
[Quantile] and therefore three copies and three sorts of the same data, for
an answer that needs one.

The copy is ordered by a radix sort over the IEEE-754 bit patterns rather
than by comparison, which is linear in the length of the column instead of
n·log n and does not call a comparator through an interface.

Every quantile is NaN if x is empty or holds a NaN, and an individual
quantile is NaN if its q is outside [0,1] — the same rule [Quantile]
follows, applied element by element. Use [DropNaN] first to summarise a
column that has gaps in it.

<small>Defined in <code>stats/stats.go</code>.</small>

### RollingFolds

```go
func RollingFolds(n, k int) []Fold
```

RollingFolds returns k expanding-window folds over n ordered observations:
train on everything up to a point, validate on what comes next, then move the
point forward.

This is cross-validation for data with an order. Standard k-fold shuffles,
which for a time series means training on Thursday to predict Wednesday — the
scores look excellent and mean nothing. Each fold here only ever trains on
data that precedes its validation window.

It returns nil when n is too small to make k folds that each have something
to train on and something to test.

<small>Defined in <code>stats/prep.go</code>.</small>

### Slope

```go
func Slope(y []float64) float64
```

Slope is the least-squares slope of y against its index (0, 1, 2, …): the
same number as Trend(y).Slope, bit for bit, without the significance test.

Trend's test reads the series a second time for the residuals and then
evaluates a t-distribution tail, and on a short series that tail is most of
the cost. A caller that only wants the direction and size of the change --
a rate of drift, a projection -- should not pay for a p-value it discards.
It cannot say whether the slope is real, only what it is; for that, Trend.

Fewer than two values give 0, as Trend does, and a NaN anywhere gives NaN.

<small>Defined in <code>stats/significance.go</code>.</small>

### Spearman

```go
func Spearman(x, y []float64) (float64, error)
```

Spearman is the rank correlation: Pearson applied to the ranks, with ties
given their average rank. It detects any monotonic relationship, straight
or not.

Values that are equal tie, and a tied group takes the average of the ranks
it spans. -0 ties with +0, because they are the same number. Every NaN
counts as one value, tied with the other NaNs and ranked above every number.

<small>Defined in <code>stats/stats.go</code>.</small>

### Split

```go
func Split(x []float64, frac float64) (train, test []float64)
```

Split cuts x into a training and a test part at [SplitIndex], preserving
order. The returned slices share x's backing array; copy them before
modifying either one.

<small>Defined in <code>stats/prep.go</code>.</small>

### SplitIndex

```go
func SplitIndex(n int, frac float64) int
```

SplitIndex returns the index at which to cut a series so that `frac` of it is
training data, for an ORDERED split.

Use this for time series and anything else where order carries information.
Shuffling a time series before splitting lets the model see the future, which
inflates every score that follows and is the single most common way a
forecasting result turns out to be wrong.

It returns 0 when there is not enough data to leave at least one observation
on each side.

<small>Defined in <code>stats/prep.go</code>.</small>

### Standardiser.Inverse

```go
func (s *Standardiser) Inverse(z []float64) ([]float64, error)
```

Inverse undoes Transform, mapping standardised values back to the original
units. Use it to report a prediction in the units a person understands.

<small>Defined in <code>stats/prep.go</code>.</small>

### Standardiser.Transform

```go
func (s *Standardiser) Transform(x []float64) ([]float64, error)
```

Transform applies the fitted scaling. It returns a new slice and does not
modify x. An unfitted scaler returns ErrNotFitted rather than silently
returning the input unchanged.

<small>Defined in <code>stats/prep.go</code>.</small>

### StdDev

```go
func StdDev(x []float64) float64
```

StdDev is the sample standard deviation: the square root of [Variance].

<small>Defined in <code>stats/stats.go</code>.</small>

### Trend

```go
func Trend(y []float64) TrendTest
```

Trend fits y against its index (0, 1, 2, …) by ordinary least squares and
tests the slope against zero. P is 1 when the series is too short for a
residual variance (n < 3) or is constant; an exact non-flat line has P = 0.

A slope is never "rising" just because it is not exactly zero: on noise the
fitted slope is essentially never zero. Use Rising and Falling, which ask
the test.

A NaN anywhere in y propagates: Slope, StdErr and T come back NaN and P
comes back 1, so Rising and Falling are both false. Trend does not drop or
interpolate missing values, because which of those is right depends on what
the gap means, and it is not a decision this function can make for the
caller. Clean the series first, or check the result with
math.IsNaN. The one thing Trend will not do is report a defined standard
error for a series it could not measure.

<small>Defined in <code>stats/significance.go</code>.</small>

### TrendTest.Falling

```go
func (t TrendTest) Falling(alpha float64) bool
```

Falling reports whether the series falls significantly at alpha.

<small>Defined in <code>stats/significance.go</code>.</small>

### TrendTest.Rising

```go
func (t TrendTest) Rising(alpha float64) bool
```

Rising reports whether the series rises significantly at alpha.

<small>Defined in <code>stats/significance.go</code>.</small>

### TwoSidedP

```go
func TwoSidedP(t float64, df int) float64
```

TwoSidedP returns P(|T| >= |t|) for T distributed Student-t with df degrees
of freedom, by the identity P(|T| >= |t|) = I_{df/(df+t²)}(df/2, 1/2), where
I is the regularised incomplete beta function. It returns 1 for df < 1 or a
NaN t, and 0 for an infinite t.

<small>Defined in <code>stats/significance.go</code>.</small>

### Variance

```go
func Variance(x []float64) float64
```

Variance is the SAMPLE variance, dividing by n-1. Use [PopVariance] when the
slice is the whole population rather than a sample from it. Fewer than two
values gives NaN.

<small>Defined in <code>stats/stats.go</code>.</small>
