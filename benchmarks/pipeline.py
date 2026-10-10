"""The same first-pass analysis, in the stack a data scientist would reach for.

pandas to load and group, numpy and scipy for the statistics, statsmodels for
the decomposition. Timed in two ways, because both are real:

  cold  - what a process pays from launch: imports, then the work. This is what
          a request handler, a CLI invocation or a per-tenant job pays EVERY
          time it runs.
  warm  - the work alone, with everything already imported. This is what a
          notebook pays after the first cell.
"""
import json, os, statistics, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
CSV = os.path.join(HERE, "data", "pipeline.csv")


def run_once():
    """The analysis. Returns per-stage seconds."""
    t = {}
    t0 = time.perf_counter()
    df = pd.read_csv(CSV)
    t["load"] = time.perf_counter() - t0

    t0 = time.perf_counter()
    v = df["value"].to_numpy()
    _ = (float(np.mean(v)), float(np.std(v, ddof=1)), float(np.min(v)), float(np.max(v)))
    _ = np.percentile(v, [50, 90, 99])  # all three from one pass, as runi's Quantiles does
    t["describe"] = time.perf_counter() - t0

    t0 = time.perf_counter()
    d = df["driver"].to_numpy()
    _ = sps.pearsonr(v, d)
    _ = sps.spearmanr(v, d)
    t["correlate"] = time.perf_counter() - t0

    t0 = time.perf_counter()
    _ = sps.linregress(np.arange(len(v)), v)
    t["trend"] = time.perf_counter() - t0

    t0 = time.perf_counter()
    _ = df.groupby("group")["value"].mean()
    t["groupby"] = time.perf_counter() - t0

    t0 = time.perf_counter()
    _ = seasonal_decompose(v[:4000], period=24, model="additive")
    t["decompose"] = time.perf_counter() - t0

    t["total"] = sum(t.values())
    return t


def main():
    # Cold: import cost is part of the measurement, because a process pays it.
    t0 = time.perf_counter()
    global pd, np, sps, seasonal_decompose
    import pandas as pd
    import numpy as np
    import scipy.stats as sps
    from statsmodels.tsa.seasonal import seasonal_decompose
    import_s = time.perf_counter() - t0

    first = run_once()
    cold_total = import_s + first["total"]

    # Warm: the median of several runs with everything loaded.
    runs = [run_once() for _ in range(5)]
    warm = {k: statistics.median(r[k] for r in runs) for k in runs[0]}

    res = {
        "library": "pandas + numpy + scipy + statsmodels",
        "python": sys.version.split()[0],
        "import_seconds": import_s,
        "cold_total_seconds": cold_total,
        "warm": warm,
    }
    with open(os.path.join(HERE, "results_pipeline_python.json"), "w") as fh:
        json.dump(res, fh, indent=2)
    print(json.dumps(res, indent=2))


if __name__ == "__main__":
    main()
