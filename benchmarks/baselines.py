"""The Python packages people reach for first, on the same work runi does.

Every case is one both sides genuinely implement. Data is synthetic and
generated the same way on both sides, with the same sizes, so the numbers are
comparable rather than suggestive.
"""
import io, json, platform, random, statistics, time

def med(f, reps):
    f()                      # warm
    ts = []
    for _ in range(reps):
        t = time.perf_counter(); f(); ts.append(time.perf_counter() - t)
    return statistics.median(ts)

res = {"library": "python baselines", "python": platform.python_version(),
       "machine": platform.machine()}
rng = random.Random(7)

# ---------- corpus (same shape as the Go side) ----------
VOCAB = ["term%04d" % i for i in range(2000)]
docs = [" ".join(rng.choice(VOCAB) for _ in range(120)) for _ in range(5000)]
queries = [" ".join(rng.choice(VOCAB) for _ in range(3)) for _ in range(200)]

# ---------- bm25: rank_bm25 ----------
try:
    from rank_bm25 import BM25Okapi
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
except Exception as e:
    res["bm25_error"] = str(e)[:120]

# ---------- sklearn TF-IDF, the other thing people use for this ----------
try:
    from sklearn.feature_extraction.text import TfidfVectorizer
    hold = {}
    def tfidf_fit():
        v = TfidfVectorizer()
        hold["m"] = v.fit_transform(docs); hold["v"] = v
    res["tfidf_index_s"] = med(tfidf_fit, 3)
except Exception as e:
    res["tfidf_error"] = str(e)[:120]

# ---------- correlation + trend: scipy ----------
try:
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
    res["trend_s"] = med(lambda: sps.linregress(np.arange(tn), t), 5)
    res["trend_n"] = tn
except Exception as e:
    res["scipy_error"] = str(e)[:120]

# ---------- seasonal decomposition: statsmodels ----------
try:
    import numpy as np
    from statsmodels.tsa.seasonal import seasonal_decompose
    sn = 4000
    s = (100 + 0.05*np.arange(sn) + 10*np.sin(2*np.pi*np.arange(sn)/24)
         + np.random.default_rng(17).normal(size=sn))
    res["season_decompose_s"] = med(lambda: seasonal_decompose(s, period=24, model="additive"), 3)
    res["season_n"] = sn
except Exception as e:
    res["statsmodels_season_error"] = str(e)[:120]

# ---------- avro: fastavro ----------
try:
    import fastavro
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
except Exception as e:
    res["avro_error"] = str(e)[:120]

print(json.dumps(res, indent=2))
open("results_python_cross.json", "w").write(json.dumps(res, indent=2))
