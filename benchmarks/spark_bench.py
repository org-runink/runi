"""SparkML on the same correlation runi and scipy do.

Spark is a distributed engine. On one node it is being asked to do something it
was not built for, and the number below says so: the comparison is honest only
if the JVM start-up and the job scheduling are counted, because a caller who
reaches for SparkML pays them. Both are reported separately so the reader can
see which part is which.
"""
import json, time

out = {"library": "pyspark"}
t_import = time.perf_counter()
from pyspark.sql import SparkSession
from pyspark.ml.stat import Correlation
from pyspark.ml.feature import VectorAssembler
out["import_s"] = time.perf_counter() - t_import

t_session = time.perf_counter()
spark = (SparkSession.builder.master("local[*]").appName("runi-bench")
         .config("spark.ui.enabled", "false")
         .config("spark.sql.shuffle.partitions", "8")
         .getOrCreate())
spark.sparkContext.setLogLevel("ERROR")
out["session_start_s"] = time.perf_counter() - t_session

import random
rng = random.Random(7)
n = 200_000
rows = [(rng.gauss(0, 1),) for _ in range(n)]
rows = [(x[0], 0.6 * x[0] + rng.gauss(0, 1)) for x in rows]

t_df = time.perf_counter()
df = spark.createDataFrame(rows, ["x", "y"])
va = VectorAssembler(inputCols=["x", "y"], outputCol="features")
vec = va.transform(df).select("features")
vec.cache().count()                     # materialise, so the timing below is the correlation
out["dataframe_build_s"] = time.perf_counter() - t_df

ts = []
for _ in range(3):
    t = time.perf_counter()
    Correlation.corr(vec, "features").head()
    ts.append(time.perf_counter() - t)
ts.sort()
out["pearson_s"] = ts[len(ts) // 2]
out["pearson_n"] = n
out["pearson_s_including_startup"] = out["pearson_s"] + out["session_start_s"] + out["import_s"]

spark.stop()
print(json.dumps(out, indent=2))
open("results_spark.json", "w").write(json.dumps(out, indent=2))
