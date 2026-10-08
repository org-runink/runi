"""Generate the canonical datasets both implementations read.

Written once, to CSV, so that runi and statsmodels are fitted to EXACTLY the
same numbers. Any benchmark where each side generates its own data is not a
comparison.

Model (ARIMAX(1,0,1) with one exogenous regressor):

    y_t = intercept + beta * x_t + n_t
    n_t = phi * n_{t-1} + e_t + theta * e_{t-1},   e_t ~ N(0, sigma^2)

The true parameters are known, so estimator BIAS is measurable -- which is the
only reason to use synthetic data here.
"""
import json, os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "data")
os.makedirs(OUT, exist_ok=True)

TRUTH = dict(intercept=1.0, beta=2.5, phi=0.6, theta=0.3, sigma=1.0)
SEED = 20261008


def series(n, rng):
    x = rng.normal(0.0, 1.0, n)
    e = rng.normal(0.0, TRUTH["sigma"], n + 1)
    nt = np.zeros(n)
    prev = 0.0
    for t in range(n):
        prev = TRUTH["phi"] * prev + e[t + 1] + TRUTH["theta"] * e[t]
        nt[t] = prev
    y = TRUTH["intercept"] + TRUTH["beta"] * x + nt
    return x, y


def main():
    rng = np.random.default_rng(SEED)
    meta = {"truth": TRUTH, "seed": SEED, "sets": {}}

    # (a) accuracy: 200 independent series of n=500, 6 points held out each
    trials, n, h = 200, 500, 6
    acc = np.zeros((trials, n, 2))
    for i in range(trials):
        x, y = series(n, rng)
        acc[i, :, 0] = x
        acc[i, :, 1] = y
    np.savetxt(os.path.join(OUT, "accuracy.csv"),
               acc.reshape(trials * n, 2), delimiter=",", header="x,y",
               comments="", fmt="%.10g")
    meta["sets"]["accuracy"] = dict(trials=trials, n=n, horizon=h)

    # (b) timing: one series at each length
    for n in (500, 2000, 10000):
        x, y = series(n, rng)
        np.savetxt(os.path.join(OUT, f"timing_{n}.csv"),
                   np.column_stack([x, y]), delimiter=",", header="x,y",
                   comments="", fmt="%.10g")
        meta["sets"][f"timing_{n}"] = dict(n=n)

    with open(os.path.join(OUT, "meta.json"), "w") as f:
        json.dump(meta, f, indent=2)
    print("  wrote", OUT)
    for k, v in meta["sets"].items():
        print("   ", k, v)


if __name__ == "__main__":
    main()
