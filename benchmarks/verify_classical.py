"""Check season.Classical against statsmodels.tsa.seasonal.seasonal_decompose.

Run `go run ./verify_classical` first: it writes the series and the Go
components at full precision, so both sides are given identical numbers.

Compared on the INTERIOR only -- the indices where a centred moving average is
defined. statsmodels returns NaN for the first and last period//2 points; runi
holds the nearest defined average there and documents it, so those points are
the one place the two deliberately differ.
"""
import json
import numpy as np
from statsmodels.tsa.seasonal import seasonal_decompose

CASES = [(4000, 24), (4000, 25), (500, 7), (300, 12), (48, 24), (97, 48)]
out = {}
worst = 0.0
for n, period in CASES:
    x = np.loadtxt("data/vc_x_%d_%d.csv" % (n, period))
    go = np.loadtxt("data/vc_go_%d_%d.csv" % (n, period), delimiter=",")
    gt, gs, gr = go[:, 0], go[:, 1], go[:, 2]
    sm = seasonal_decompose(x, period=period, model="additive")
    h = period // 2
    lo, hi = h, n - h
    diffs = {
        "trend": float(np.max(np.abs(gt[lo:hi] - sm.trend[lo:hi]))),
        "seasonal": float(np.max(np.abs(gs - sm.seasonal))),
        "resid": float(np.max(np.abs(gr[lo:hi] - sm.resid[lo:hi]))),
    }
    # The ends: statsmodels must be NaN exactly where we hold a value.
    diffs["sm_nan_head"] = int(np.sum(np.isnan(sm.trend[:h])))
    diffs["sm_nan_tail"] = int(np.sum(np.isnan(sm.trend[hi:])))
    diffs["runi_finite"] = bool(np.all(np.isfinite(go)))
    # And runi's own identity, everywhere including the ends.
    diffs["sum_err"] = float(np.max(np.abs(gt + gs + gr - x)))
    diffs["scale"] = float(np.max(np.abs(x)))
    out["n=%d period=%d" % (n, period)] = diffs
    worst = max(worst, diffs["trend"], diffs["seasonal"], diffs["resid"])
    assert diffs["sm_nan_head"] == h and diffs["sm_nan_tail"] == h, diffs
    assert diffs["runi_finite"]

out["worst_component_diff"] = worst
print(json.dumps(out, indent=2))
open("results_verify_classical.json", "w").write(json.dumps(out, indent=2))
print("WORST DIFF %.3e -- %s" % (worst, "OK" if worst < 1e-10 else "TOO LARGE"))
