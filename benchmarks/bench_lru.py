"""Python's functools.lru_cache on the same shape of work runi/memo does.

`functools.lru_cache` is the API that inspired `memo`: one decorator, an LRU
bound, and nothing else to configure. This measures what it costs per call and
what it does under a stampede, so the Go figures have something to sit beside.

The comparison is deliberately narrow, because the two are not equivalent:

  - lru_cache has NO TTL and NO single-flight. Under concurrent misses it runs
    the function once per caller. memo runs it once.
  - lru_cache keys on the argument tuple's hash; memo.Hash canonicalises
    structured keys (sorted map order, type tags, length prefixes).
  - Python threads share one interpreter lock, so "concurrent" here means
    interleaved, not parallel. That is a property of the runtime, not a defect
    in lru_cache, and it is exactly why the stampede number differs.

Run:  python benchmarks/bench_lru.py
"""
import json
import os
import statistics
import threading
import time
from functools import lru_cache

HERE = os.path.dirname(os.path.abspath(__file__))
REPS = 200_000
WORK_MS = 5  # the "expensive" function, matched to the Go stampede benchmark


def bench_hit():
    @lru_cache(maxsize=1024)
    def f(k):
        return k * 2

    f(1)  # prime
    t0 = time.perf_counter_ns()
    for _ in range(REPS):
        f(1)
    el = time.perf_counter_ns() - t0
    return el / REPS


def bench_miss():
    @lru_cache(maxsize=1024)
    def f(k):
        return k * 2

    n = 50_000
    t0 = time.perf_counter_ns()
    for i in range(n):
        f(i)
    el = time.perf_counter_ns() - t0
    return el / n


def bench_stampede(callers):
    """N threads hit one cold key at once. Counts how many times f actually ran."""
    calls = []
    lock = threading.Lock()

    @lru_cache(maxsize=1024)
    def f(k):
        with lock:
            calls.append(1)
        time.sleep(WORK_MS / 1000.0)
        return k * 2

    barrier = threading.Barrier(callers)

    def worker():
        barrier.wait()
        f("cold")

    threads = [threading.Thread(target=worker) for _ in range(callers)]
    t0 = time.perf_counter()
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    el = time.perf_counter() - t0
    return dict(wall_seconds=el, function_ran_times=len(calls), callers=callers)


def main():
    hit = statistics.median([bench_hit() for _ in range(5)])
    miss = statistics.median([bench_miss() for _ in range(5)])
    res = {
        "library": "python functools.lru_cache",
        "python": os.sys.version.split()[0],
        "hit_ns": hit,
        "miss_ns": miss,
        "stampede_8": bench_stampede(8),
        "stampede_64": bench_stampede(64),
        "has_ttl": False,
        "has_single_flight": False,
        "work_ms": WORK_MS,
    }
    with open(os.path.join(HERE, "results_lru.json"), "w") as fh:
        json.dump(res, fh, indent=2)
    print(json.dumps(res, indent=2))


if __name__ == "__main__":
    main()
