"""The Python half of restbench: the same five workloads, in what a Python
author would actually reach for.

  salvage   -> json.JSONDecoder.raw_decode, scanning for the first valid value
  chain     -> hashlib sha256 over (prev, body)
  lazy      -> concurrent.futures.ThreadPoolExecutor
  toon      -> json (there is no TOON in Python; the comparison is against the
               format it replaces, on both time and SIZE)
  tablelog  -> sqlite3, which is what you reach for when you want versioned
               rows without standing up a database

budget has no counterpart and none is invented here.
"""
import concurrent.futures as cf
import hashlib
import json
import os
import sqlite3
import statistics
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))


def med(f, reps):
    f()  # warm
    out = []
    for _ in range(reps):
        t = time.perf_counter()
        f()
        out.append(time.perf_counter() - t)
    return statistics.median(out)


def replies(n):
    return [
        "Sure — here is the result you asked for.\n\n```json\n"
        '{"id":%d,"verdict":"pass","score":%0.3f,"notes":"line %d of the reply"}'
        "\n```\n\nLet me know if you need anything else." % (i, i * 0.37, i)
        for i in range(n)
    ]


def salvage_one(text, dec=json.JSONDecoder()):
    """Find the first balanced JSON value in free text. This is the loop you
    write when you do not have a library for it: walk to each '{', try to
    decode there, move on if it fails."""
    i = 0
    while True:
        i = text.find("{", i)
        if i < 0:
            return None
        try:
            obj, _ = dec.raw_decode(text, i)
            return obj
        except ValueError:
            i += 1


def main():
    res = {"library": "python stdlib", "python": sys.version.split()[0]}

    texts = replies(2000)
    res["salvage_n"] = len(texts)
    res["salvage_s"] = med(lambda: [salvage_one(t) for t in texts], 5)

    nrec = 20000
    bodies = [('{"event":%d,"actor":"svc","action":"write"}' % i).encode() for i in range(nrec)]
    DOMAIN = b"runi/chain v1\x00"

    def seal_all():
        out, prev = [], ""
        for b in bodies:
            h = hashlib.sha256()
            h.update(DOMAIN); h.update(prev.encode()); h.update(b"\x00"); h.update(b)
            prev = h.hexdigest()
            out.append((prev, b))
        return out

    res["chain_n"] = nrec
    res["chain_seal_s"] = med(seal_all, 5)
    links = seal_all()

    def verify_all():
        prev = ""
        for got, b in links:
            h = hashlib.sha256()
            h.update(DOMAIN); h.update(prev.encode()); h.update(b"\x00"); h.update(b)
            if h.hexdigest() != got:
                raise AssertionError("broken")
            prev = got

    res["chain_verify_s"] = med(verify_all, 5)

    res["lazy_values"] = 5
    res["lazy_each_ms"] = 80

    def five_together():
        with cf.ThreadPoolExecutor(max_workers=5) as ex:
            list(ex.map(lambda i: (time.sleep(0.080), i)[1], range(5)))

    res["lazy_s"] = med(five_together, 3)

    rows = [
        {"id": i, "severity": ["low", "high", "medium"][i % 3],
         "file": "internal/pkg%d/file.go" % (i % 40), "line": i % 900}
        for i in range(2000)
    ]
    doc = {"findings": rows}
    res["toon_rows"] = len(rows)
    res["json_encode_s"] = med(lambda: json.dumps(doc), 5)
    text = json.dumps(doc)
    res["json_bytes"] = len(text)
    res["json_decode_s"] = med(lambda: json.loads(text), 5)

    trows, tbatch = 10000, 100

    def sqlite_write():
        con = sqlite3.connect(":memory:")
        con.execute("CREATE TABLE events (key TEXT, payload TEXT, version INTEGER)")
        v = 0
        batch = []
        for i in range(trows):
            batch.append(("k%06d" % i, '{"event":%d,"kind":"write"}' % i, v))
            if len(batch) == tbatch:
                v += 1
                con.executemany("INSERT INTO events VALUES (?,?,?)", batch)
                con.commit()
                batch = []
        con.close()

    res["tablelog_rows"] = trows
    res["tablelog_write_s"] = med(sqlite_write, 3)

    con = sqlite3.connect(":memory:")
    con.execute("CREATE TABLE events (key TEXT, payload TEXT, version INTEGER)")
    con.executemany("INSERT INTO events VALUES (?,?,?)",
                    [("k%06d" % i, '{"x":1}', i // tbatch) for i in range(trows)])
    con.commit()
    res["tablelog_read_s"] = med(lambda: con.execute("SELECT key,payload FROM events").fetchall(), 3)

    res["budget_note"] = "no counterpart benchmarked"

    with open(os.path.join(HERE, "results_rest_python.json"), "w") as fh:
        json.dump(res, fh, indent=2)
    print(json.dumps(res, indent=2))


if __name__ == "__main__":
    main()
