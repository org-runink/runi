// Command genapi generates website/content/docs/api/ from the package sources.
//
//	go run ./internal/genapi            (from the repository root)
//	go run ./internal/genapi -root DIR  (another checkout)
//
// The API Reference pages say they are generated from the source "so it cannot
// drift from it". That is only true because .github/workflows/docs.yml re-runs
// this command and fails if the result differs from what is committed: without
// that check the sentence would be an aspiration, not a property.
//
// It replaces website/gen-api.py (a Python script the repository no longer
// needs: nothing else here uses Python) and writes the same bytes: the port was
// checked by regenerating the pages at the commit where they were last current
// and requiring no difference.
//
// Scope and limits, stated because they bound what the pages can be trusted to
// say: this reads the source as TEXT, not through go/ast. It finds exported
// top-level declarations anchored at the start of a line, so it cannot see
// anything inside a grouped `const (` / `var (` block, and it takes the first
// line of a signature only. pkg.go.dev remains the canonical reference and
// every page links to it.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var pkgs = []string{"stats", "arimax", "season", "bm25", "salvage", "avro", "barn", "memo", "lazy",
	"budget", "chain", "toon", "toml", "drift", "rate", "tree", "sustain", "throttle"}

var blurb = map[string]string{
	"stats":    "Describe, split and scale a column, and test whether a relationship is real",
	"arimax":   "Forecast a series using the things that drive it",
	"season":   "Find the season and the trend breaks, then split the series apart",
	"drift":    "Streaming change detection over a moving baseline",
	"sustain":  "Threshold decisions that do not flap",
	"throttle": "Cap how often something acts",
	"rate":     "Cumulative counters into per-interval rates, across wraps and resets",
	"budget":   "One deadline for a whole request, split across its phases",
	"memo":     "Memoise an expensive call, one allocation per miss",
	"lazy":     "Compute once, on first use, safely under concurrency",
	"tree":     "Subtree sums and maxima of a hierarchy, kept current",
	"bm25":     "Rank documents by the words they share",
	"salvage":  "Get JSON out of a language model's reply, without guessing",
	"toon":     "A compact object notation that costs fewer tokens than JSON",
	"toml":     "Read TOML v1.0.0 configuration files into Go values, strictly if asked",
	"avro":     "Read and write Avro and Avro OCF files",
	"barn":     "Append-only versioned tables with time travel, no database",
	"chain":    "Tamper-evident chaining and verification of records",
}

// Exported top-level declarations ONLY. Each alternative anchors the capital letter at the real
// start of the identifier, so `func encodeOptions(` cannot match.
var (
	reFunc   = regexp.MustCompile(`^func\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]`)
	reMethod = regexp.MustCompile(`^func\s+\(\s*\w+\s+\*?([A-Z][A-Za-z0-9_]*)[^\)]*\)\s+([A-Z][A-Za-z0-9_]*)\s*[\(\[]`)
	reType   = regexp.MustCompile(`^type\s+([A-Z][A-Za-z0-9_]*)[\s\[]`)
	reCV     = regexp.MustCompile(`^(?:const|var)\s+([A-Z][A-Za-z0-9_]*)[\s=]`)
)

type decl struct{ name, kind, sig, doc, file string }

// commentText drops the leading "// " of a comment line the way the original did: the first three
// characters when the line has more than two, else nothing.
func commentText(ln string) string {
	r := []rune(ln)
	if len(r) > 2 {
		return string(r[3:])
	}
	return ""
}

func rstrip(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

func stripBody(sig string) string {
	s := rstrip(sig)
	if strings.HasSuffix(s, "}") { // one-line body: func F() T { ... }
		if i := strings.Index(s, "{"); i > 0 {
			s = rstrip(s[:i])
		}
	} else if strings.HasSuffix(s, "{") {
		s = rstrip(s[:len(s)-1])
	}
	return s
}

func srcFiles(root, p string) []string {
	ents, err := os.ReadDir(filepath.Join(root, p))
	if err != nil {
		die(err)
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func lines(root, p, f string) []string {
	b, err := os.ReadFile(filepath.Join(root, p, f))
	if err != nil {
		die(err)
	}
	return strings.Split(string(b), "\n")
}

func pkgDoc(root, p string) string {
	for _, f := range srcFiles(root, p) {
		var buf []string
		started := false
		for _, ln := range lines(root, p, f) {
			switch {
			case strings.HasPrefix(ln, "// Package "+p) || (started && strings.HasPrefix(ln, "//")):
				buf = append(buf, commentText(ln))
				started = true
			case started && strings.HasPrefix(ln, "package "):
				return strings.TrimSpace(strings.Join(buf, "\n"))
			case started:
				buf, started = nil, false
			}
		}
	}
	return ""
}

func decls(root, p string) []decl {
	var out []decl
	seen := map[string]bool{}
	for _, f := range srcFiles(root, p) {
		var doc []string
		for _, ln := range lines(root, p, f) {
			if strings.HasPrefix(ln, "//") {
				doc = append(doc, commentText(ln))
				continue
			}
			name, kind := "", ""
			if m := reMethod.FindStringSubmatch(ln); m != nil {
				name, kind = m[1]+"."+m[2], "method"
			} else if m := reFunc.FindStringSubmatch(ln); m != nil {
				name, kind = m[1], "func"
			} else if m := reType.FindStringSubmatch(ln); m != nil {
				name, kind = m[1], "type"
			} else if m := reCV.FindStringSubmatch(ln); m != nil {
				name, kind = m[1], "const"
			}
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, decl{name, kind, stripBody(ln), strings.TrimSpace(strings.Join(doc, "\n")), f})
			}
			doc = nil
		}
	}
	// Types first, then by lower-cased name; stable, so equal keys keep source order.
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].kind != "type", out[j].kind != "type"
		if ti != tj {
			return !ti
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "genapi:", err)
	os.Exit(1)
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	outDir := filepath.Join(*root, "website", "content", "docs", "api")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		die(err)
	}
	total := 0
	for i, p := range pkgs {
		ds, pd := decls(*root, p), pkgDoc(*root, p)
		total += len(ds)
		b := []string{
			fmt.Sprintf("---\ntitle: \"runi/%s\"\nweight: %d\n---\n", p, i+1),
			fmt.Sprintf("```go\nimport \"github.com/org-runink/runi/%s\"\n```\n", p),
		}
		if pd != "" {
			b = append(b, pd+"\n")
		}
		b = append(b, "## Index\n")
		for _, d := range ds {
			anchor := strings.ReplaceAll(strings.ToLower(d.name), ".", "")
			b = append(b, fmt.Sprintf("- `%s` [%s](#%s)", d.kind, d.name, anchor))
		}
		b = append(b, "")
		for _, d := range ds {
			b = append(b, fmt.Sprintf("### %s\n", d.name))
			b = append(b, fmt.Sprintf("```go\n%s\n```\n", d.sig))
			if d.doc != "" {
				b = append(b, d.doc+"\n")
			}
			b = append(b, fmt.Sprintf("<small>Defined in <code>%s/%s</code>.</small>\n", p, d.file))
		}
		if err := os.WriteFile(filepath.Join(outDir, p+".md"), []byte(strings.Join(b, "\n")), 0o644); err != nil {
			die(err)
		}
		fmt.Printf("  %-10s %3d\n", p, len(ds))
	}
	idx := []string{
		"---\ntitle: \"API Reference\"\nweight: 3\nsidebar:\n  open: true\n---\n",
		"Every exported symbol in every package, generated from the source so it cannot drift from it.",
		"For the canonical reference, including example output and unexported detail,",
		"see [pkg.go.dev](https://pkg.go.dev/github.com/org-runink/runi).\n",
		"| Package | What it does |", "| --- | --- |",
	}
	for _, p := range pkgs {
		idx = append(idx, fmt.Sprintf("| [`runi/%s`](%s/) | %s |", p, p, blurb[p]))
	}
	if err := os.WriteFile(filepath.Join(outDir, "_index.md"), []byte(strings.Join(idx, "\n")+"\n"), 0o644); err != nil {
		die(err)
	}
	fmt.Println("total exported symbols:", total)
}
