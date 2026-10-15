"""Generate website/content/docs/api/ from the package sources.

    python3 -I website/gen-api.py

The API Reference pages say they are generated from the source "so it cannot drift
from it". That is only true because .github/workflows/docs.yml re-runs this script
and fails if the result differs from what is committed -- without that check the
sentence would be an aspiration, not a property.

Scope and limits, stated because they bound what the pages can be trusted to say:
this reads the source as TEXT, not through go/ast. It finds exported top-level
declarations anchored at the start of a line, so it cannot see anything inside a
grouped `const (` / `var (` block, and it takes the first line of a signature only.
pkg.go.dev remains the canonical reference and every page links to it.

Paths are resolved from this file's own location, so it works from any directory.
"""
import os, re

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
OUT  = os.path.join(HERE, "content/docs/api")
PKGS = ["stats","arimax","season","bm25","salvage","avro","barn","memo","lazy",
        "budget","chain","toon","drift","rate","tree","sustain","throttle"]
BLURB = {
 "stats":"Describe, split and scale a column, and test whether a relationship is real",
 "arimax":"Forecast a series using the things that drive it",
 "season":"Find the season and the trend breaks, then split the series apart",
 "drift":"Streaming change detection over a moving baseline",
 "sustain":"Threshold decisions that do not flap",
 "throttle":"Cap how often something acts",
 "rate":"Cumulative counters into per-interval rates, across wraps and resets",
 "budget":"One deadline for a whole request, split across its phases",
 "memo":"Memoise an expensive call, one allocation per miss",
 "lazy":"Compute once, on first use, safely under concurrency",
 "tree":"Subtree sums and maxima of a hierarchy, kept current",
 "bm25":"Rank documents by the words they share",
 "salvage":"Get JSON out of a language model's reply, without guessing",
 "toon":"A compact object notation that costs fewer tokens than JSON",
 "avro":"Read and write Avro and Avro OCF files",
 "barn":"Append-only versioned tables with time travel, no database",
 "chain":"Tamper-evident chaining and verification of records",
}

# Exported top-level declarations ONLY. Each alternative anchors the capital letter
# at the real start of the identifier, so `func encodeOptions(` cannot match.
RE_FUNC   = re.compile(r'^func\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]')
RE_METHOD = re.compile(r'^func\s+\(\s*\w+\s+\*?([A-Z][A-Za-z0-9_]*)[^\)]*\)\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]')
RE_TYPE   = re.compile(r'^type\s+([A-Z][A-Za-z0-9_]*)[\s\[]')
RE_CV     = re.compile(r'^(?:const|var)\s+([A-Z][A-Za-z0-9_]*)[\s=]')

def strip_body(sig):
    s = sig.rstrip()
    if s.endswith("}"):                       # one-line body: func F() T { ... }
        i = s.find("{")
        if i > 0: s = s[:i].rstrip()
    elif s.endswith("{"):
        s = s[:-1].rstrip()
    return s

def srcfiles(p):
    d = os.path.join(ROOT, p)
    return sorted(f for f in os.listdir(d) if f.endswith(".go") and not f.endswith("_test.go"))

def pkgdoc(p):
    for f in srcfiles(p):
        buf = []
        for ln in open(os.path.join(ROOT,p,f), encoding="utf-8").read().split("\n"):
            if ln.startswith("// Package "+p) or (buf and ln.startswith("//")):
                buf.append(ln[3:] if len(ln) > 2 else "")
            elif buf and ln.startswith("package "):
                return "\n".join(buf).strip()
            elif buf:
                buf = []
    return ""

def decls(p):
    out, seen = [], set()
    for f in srcfiles(p):
        doc = []
        for ln in open(os.path.join(ROOT,p,f), encoding="utf-8").read().split("\n"):
            if ln.startswith("//"):
                doc.append(ln[3:] if len(ln) > 2 else ""); continue
            name = kind = None
            m = RE_METHOD.match(ln)
            if m: name, kind = f"{m.group(1)}.{m.group(2)}", "method"
            else:
                m = RE_FUNC.match(ln)
                if m: name, kind = m.group(1), "func"
                else:
                    m = RE_TYPE.match(ln)
                    if m: name, kind = m.group(1), "type"
                    else:
                        m = RE_CV.match(ln)
                        if m: name, kind = m.group(1), "const"
            if name and name not in seen:
                seen.add(name)
                out.append((name, kind, strip_body(ln), "\n".join(doc).strip(), f))
            doc = []
    out.sort(key=lambda d: (d[1] != "type", d[0].lower()))
    return out

os.makedirs(OUT, exist_ok=True)
total = 0
for i, p in enumerate(PKGS, 1):
    ds, pd = decls(p), pkgdoc(p)
    total += len(ds)
    b = [f'---\ntitle: "runi/{p}"\nweight: {i}\n---\n',
         f'```go\nimport "github.com/org-runink/runi/{p}"\n```\n']
    if pd: b.append(pd + "\n")
    b.append("## Index\n")
    for name, kind, _, _, _ in ds:
        anchor = name.lower().replace(".", "")
        b.append(f"- `{kind}` [{name}](#{anchor})")
    b.append("")
    for name, kind, sig, doc, f in ds:
        b.append(f"### {name}\n")
        b.append(f"```go\n{sig}\n```\n")
        if doc: b.append(doc + "\n")
        b.append(f"<small>Defined in <code>{p}/{f}</code>.</small>\n")
    open(os.path.join(OUT, f"{p}.md"), "w", encoding="utf-8").write("\n".join(b))
    print(f"  {p:10} {len(ds):3}")

idx = ['---\ntitle: "API Reference"\nweight: 3\nsidebar:\n  open: true\n---\n',
  "Every exported symbol in every package, generated from the source so it cannot drift from it.",
  "For the canonical reference, including example output and unexported detail,",
  "see [pkg.go.dev](https://pkg.go.dev/github.com/org-runink/runi).\n",
  "| Package | What it does |", "| --- | --- |"]
for p in PKGS:
    idx.append(f"| [`runi/{p}`]({p}/) | {BLURB[p]} |")
open(os.path.join(OUT,"_index.md"),"w",encoding="utf-8").write("\n".join(idx)+"\n")
print("total exported symbols:", total)
