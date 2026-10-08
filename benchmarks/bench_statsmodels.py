"""statsmodels SARIMAX on the canonical datasets. Writes results_statsmodels.json."""
import json, os, time, warnings
import numpy as np
warnings.filterwarnings("ignore")

t_import0 = time.perf_counter()
import statsmodels.api as sm
from statsmodels.tsa.statespace.sarimax import SARIMAX
T_IMPORT = time.perf_counter() - t_import0

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")
META = json.load(open(os.path.join(DATA, "meta.json")))
TRUTH = META["truth"]
ORDER = (1, 0, 1)


def fit(y, x):
    m = SARIMAX(y, exog=x, order=ORDER, trend="c",
                enforce_stationarity=False, enforce_invertibility=False)
    return m.fit(disp=False)


def accuracy():
    cfg = META["sets"]["accuracy"]
    trials, n, h = cfg["trials"], cfg["n"], cfg["horizon"]
    raw = np.loadtxt(os.path.join(DATA, "accuracy.csv"), delimiter=",", skiprows=1)
    raw = raw.reshape(trials, n, 2)

    b_err, p_err, cover, hits, rmse_f, rmse_naive = [], [], 0, 0, [], []
    t0 = time.perf_counter()
    for i in range(trials):
        x, y = raw[i, :, 0], raw[i, :, 1]
        xtr, ytr = x[: n - h], y[: n - h]
        xte, yte = x[n - h:], y[n - h:]
        try:
            r = fit(ytr, xtr)
        except Exception:
            continue
        # statsmodels names: 'x1' for the exog, 'ar.L1' for phi
        pr = dict(zip(r.param_names, r.params))
        b_err.append(pr.get("x1", np.nan) - TRUTH["beta"])
        p_err.append(pr.get("ar.L1", np.nan) - TRUTH["phi"])

        fc = r.get_forecast(steps=h, exog=xte.reshape(-1, 1))
        pt = np.asarray(fc.predicted_mean)
        ci = np.asarray(fc.conf_int(alpha=0.05))
        lo, hi = ci[:, 0], ci[:, 1]
        cover += int(((yte >= lo) & (yte <= hi)).sum())
        hits += h
        rmse_f.append(float(np.sqrt(np.mean((yte - pt) ** 2))))
        rmse_naive.append(float(np.sqrt(np.mean((yte - ytr[-1]) ** 2))))
    el = time.perf_counter() - t0

    b, p = np.array(b_err), np.array(p_err)
    return dict(
        trials=len(b_err), seconds_total=el, seconds_per_fit=el / max(len(b_err), 1),
        beta_bias=float(np.nanmean(b)), beta_rmse=float(np.sqrt(np.nanmean(b ** 2))),
        phi_bias=float(np.nanmean(p)), phi_rmse=float(np.sqrt(np.nanmean(p ** 2))),
        coverage_pct=100.0 * cover / max(hits, 1), coverage_points=hits,
        forecast_rmse=float(np.mean(rmse_f)), naive_rmse=float(np.mean(rmse_naive)),
    )


def timing():
    out = {}
    for n in (500, 2000, 10000):
        raw = np.loadtxt(os.path.join(DATA, f"timing_{n}.csv"), delimiter=",", skiprows=1)
        x, y = raw[:, 0], raw[:, 1]
        reps = 10 if n <= 2000 else 3
        fit(y, x)  # warm up: JIT-free, but caches BLAS threads etc.
        ts = []
        for _ in range(reps):
            t0 = time.perf_counter(); fit(y, x); ts.append(time.perf_counter() - t0)
        out[str(n)] = dict(seconds_median=float(np.median(ts)), reps=reps)
    return out


if __name__ == "__main__":
    res = dict(
        library="statsmodels", version=sm.__version__,
        estimator="exact MLE via Kalman filter (SARIMAX default)",
        import_seconds=T_IMPORT, accuracy=accuracy(), timing=timing(),
    )
    with open(os.path.join(HERE, "results_statsmodels.json"), "w") as f:
        json.dump(res, f, indent=2)
    print(json.dumps(res, indent=2))
