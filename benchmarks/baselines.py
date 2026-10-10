"""The Python packages people reach for first, on the same work runi does.

Every case is one both sides genuinely implement. Data is synthetic and
generated the same way on both sides, with the same sizes, so the numbers are
comparable rather than suggestive.

**Each library is measured in its own process.** That is not fastidiousness.
When all of these ran in one interpreter, `scipy.stats.pearsonr` on 200,000
points measured 0.80 ms if scikit-learn had been imported earlier in the same
process and 3.3-6.5 ms if it had not -- a factor of four, decided by import
order rather than by the code being timed. Importing scikit-learn pulls in
BLAS/OpenMP machinery and warms allocator and page-cache state that the later
measurements then inherit. A table built that way is not reproducible, and the
first person to run one section on its own gets a different answer than the
table claims.

So: `python3 baselines.py` runs every section as a separate subprocess and
merges the results; `python3 baselines.py <section>` runs exactly one and
prints it. The fixtures are rebuilt identically in each section, so the data
every library sees is what it saw before.

Sections: bm25 tfidf scipy season avro
"""
import io, json, os, platform, random, statistics, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
SECTIONS = ["bm25", "tfidf", "scipy", "season", "avro"]


def med(f, reps):
    f()                      # warm
    ts = []
    for _ in range(reps):
        t = time.perf_counter(); f(); ts.append(time.perf_counter() - t)
    return statistics.median(ts)


def corpus():
    """The shared fixture. Rebuilt per section so each one draws the same
    numbers from the same generator state the single-process script used."""
    rng = random.Random(7)
    VOCAB = ["term%04d" % i for i in range(2000)]
    docs = [" ".join(rng.choice(VOCAB) for _ in range(120)) for _ in range(5000)]
    queries = [" ".join(rng.choice(VOCAB) for _ in range(3)) for _ in range(200)]
    return rng, docs, queries


def sec_bm25(res):
    from rank_bm25 import BM25Okapi
    _, docs, queries = corpus()
    tok = [d.split() for d in docs]
    qtok = [q.split() for q in queries]
    holder = {}
    def build(): holder["ix"] = BM25Okapi(tok)
    res["bm25_index_s"] = med(build, 3)
    def query():
        ix = holder["ix"]
        for q in qtok: ix.get_top_n(q, docs, n=10)
    res["bm25_query_s"] = med(query, 3)
    res["bm25_lib"] = "rank_bm25"


def sec_tfidf(res):
    from sklearn.feature_extraction.text import TfidfVectorizer
    _, docs, _ = corpus()
    hold = {}
    def tfidf_fit():
        v = TfidfVectorizer()
        hold["m"] = v.fit_transform(docs); hold["v"] = v
    res["tfidf_index_s"] = med(tfidf_fit, 3)


def sec_scipy(res):
    import numpy as np
    from scipy import stats as sps
    n = 200_000
    x = np.random.default_rng(7).normal(size=n)
    y = 0.6 * x + np.random.default_rng(11).normal(size=n)
    res["pearson_s"] = med(lambda: sps.pearsonr(x, y), 5)
    res["spearman_s"] = med(lambda: sps.spearmanr(x, y), 3)
    res["pearson_n"] = n
    tn = 100_000
    t = 0.001 * np.arange(tn) + np.random.default_rng(13).normal(size=tn)
    # xs is built ONCE, outside the timed call. It used to be built inside it,
    # which meant every timed call allocated a fresh 800 KB array -- so the
    # figure was linregress plus an allocation, measured against a Go side that
    # builds its series before the timer starts. It was also bimodal: ~0.38 ms
    # when the allocator could reuse a warm block and ~8.99 ms when the
    # sections before it had churned memory and each call faulted in its pages.
    # That is where this table's old "33x" on trend came from.
    xs = np.arange(tn)
    res["trend_s"] = med(lambda: sps.linregress(xs, t), 5)
    res["trend_n"] = tn


def sec_season(res):
    import numpy as np
    from statsmodels.tsa.seasonal import seasonal_decompose
    sn = 4000
    s = (100 + 0.05 * np.arange(sn) + 10 * np.sin(2 * np.pi * np.arange(sn) / 24)
         + np.random.default_rng(17).normal(size=sn))
    res["season_decompose_s"] = med(
        lambda: seasonal_decompose(s, period=24, model="additive"), 3)
    res["season_n"] = sn


def sec_avro(res):
    import fastavro
    rng, _, _ = corpus()
    schema = {"type": "record", "name": "R", "fields": [
        {"name": "name", "type": "string"},
        {"name": "vals", "type": {"type": "array", "items": "double"}}]}
    rows = [{"name": "row-%06d" % i, "vals": [rng.random(), rng.random(), rng.random()]}
            for i in range(20_000)]
    buf = {}
    def write():
        b = io.BytesIO(); fastavro.writer(b, schema, rows, codec="null"); buf["b"] = b.getvalue()
    res["avro_write_s"] = med(write, 3)
    res["avro_bytes"] = len(buf["b"])
    def read():
        list(fastavro.reader(io.BytesIO(buf["b"])))
    res["avro_read_s"] = med(read, 3)
    res["avro_rows"] = len(rows)


def run_one(name):
    res = {}
    try:
        globals()["sec_" + name](res)
    except Exception as e:                      # a missing package is a result
        res[name + "_error"] = str(e)[:120]
    return res


def main():
    if len(sys.argv) > 1:
        name = sys.argv[1]
        if name not in SECTIONS:
            sys.exit("unknown section %r; want one of %s" % (name, " ".join(SECTIONS)))
        print(json.dumps(run_one(name), indent=2))
        return

    res = {"library": "python baselines", "python": platform.python_version(),
           "machine": platform.machine(), "isolation": "one process per section"}
    for name in SECTIONS:
        # The children inherit this process's CPU affinity, so
        # `taskset ... baselines.py` pins the processes doing the measuring.
        out = subprocess.run([sys.executable, os.path.abspath(__file__), name],
                             capture_output=True, text=True, cwd=HERE)
        if out.returncode != 0:
            res[name + "_error"] = (out.stderr or "").strip()[:120]
            continue
        res.update(json.loads(out.stdout))
    print(json.dumps(res, indent=2))
    with open(os.path.join(HERE, "results_python_cross.json"), "w") as fh:
        fh.write(json.dumps(res, indent=2))


if __name__ == "__main__":
    main()
