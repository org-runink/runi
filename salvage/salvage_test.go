// SPDX-License-Identifier: BSD-3-Clause

package salvage

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type card struct {
	Title  string  `json:"title"`
	Impact float64 `json:"impact"`
}

// The shapes real model replies come in.
func TestDecodeRealisticReplies(t *testing.T) {
	want := card{Title: "Reroute lane 4", Impact: 1200}
	for name, reply := range map[string]string{
		"bare":            `{"title":"Reroute lane 4","impact":1200}`,
		"fenced":          "```json\n{\"title\":\"Reroute lane 4\",\"impact\":1200}\n```",
		"fence, no tag":   "```\n{\"title\":\"Reroute lane 4\",\"impact\":1200}\n```",
		"preamble":        "Sure! Here is the card:\n{\"title\":\"Reroute lane 4\",\"impact\":1200}",
		"closing remark":  "{\"title\":\"Reroute lane 4\",\"impact\":1200}\nLet me know if you need more.",
		"brace in prose":  "Use {braces} carefully. {\"title\":\"Reroute lane 4\",\"impact\":1200}",
		"brace in string": "Result: {\"title\":\"Reroute lane 4\",\"impact\":1200,\"note\":\"} not the end {\"}",
		"escaped quote":   `{"title":"Reroute lane 4","impact":1200,"note":"say \"}\" twice"}`,
		"surrounding ws":  "\n\t  {\"title\":\"Reroute lane 4\",\"impact\":1200}  \n",
	} {
		t.Run(name, func(t *testing.T) {
			var got card
			if err := Decode(reply, &got); err != nil || got != want {
				t.Fatalf("Decode = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

// Two values: the first that fits the destination wins, and a value that does
// not fit is skipped rather than half-applied.
func TestDecodeSkipsValuesThatDoNotFit(t *testing.T) {
	reply := `First the scores: [1, 2, 3]. Then {"impact": "high"} (wrong type), then {"title":"B","impact":7}.`
	var got card
	if err := Decode(reply, &got); err != nil || got != (card{Title: "B", Impact: 7}) {
		t.Fatalf("Decode = %+v, %v", got, err)
	}
	var nums []int
	if err := Decode(reply, &nums); err != nil || !reflect.DeepEqual(nums, []int{1, 2, 3}) {
		t.Fatalf("array: %v, %v", nums, err)
	}
}

func TestDecodeReplacesOnlyOnSuccess(t *testing.T) {
	got := card{Title: "kept", Impact: 1}
	if err := Decode(`{"title": 5}`, &got); !errors.Is(err, ErrNoJSON) {
		t.Fatalf("err = %v, want ErrNoJSON", err)
	}
	if got != (card{Title: "kept", Impact: 1}) {
		t.Errorf("a failed decode changed the destination: %+v", got)
	}
	if err := Decode(`{"title":"new"}`, &got); err != nil || got != (card{Title: "new"}) {
		t.Errorf("success must replace, not merge: %+v, %v", got, err)
	}
}

func TestDecodeStrictRejectsOtherShapes(t *testing.T) {
	reply := `{"title":"x","impact":1,"severity":"high"} {"title":"y","impact":2}`
	var loose, strict card
	if err := Decode(reply, &loose); err != nil || loose.Title != "x" {
		t.Errorf("Decode ignores unknown fields: %+v, %v", loose, err)
	}
	if err := DecodeStrict(reply, &strict); err != nil || strict.Title != "y" {
		t.Errorf("DecodeStrict must skip the value with an unknown field: %+v, %v", strict, err)
	}
	if err := DecodeStrict(`{"other":1}`, &strict); !errors.Is(err, ErrNoJSON) {
		t.Errorf("no matching shape: %v", err)
	}
}

// It extracts; it never repairs.
func TestDecodeDoesNotRepair(t *testing.T) {
	for name, reply := range map[string]string{
		"trailing comma": `{"title":"x","impact":1,}`,
		"single quotes":  `{'title':'x'}`,
		"unquoted key":   `{title:"x"}`,
		"comment":        `{"title":"x" /* note */}`,
		"cut off":        `Here: {"title":"x","impact":`,
		"no json":        `I could not produce a card for this.`,
		"empty":          ``,
	} {
		t.Run(name, func(t *testing.T) {
			var got card
			if err := Decode(reply, &got); !errors.Is(err, ErrNoJSON) {
				t.Errorf("Decode(%q) = %+v, %v; want ErrNoJSON", reply, got, err)
			}
		})
	}
}

func TestDecodeNeedsAPointer(t *testing.T) {
	var c card
	for _, v := range []any{nil, c, (*card)(nil)} {
		if err := Decode(`{"title":"x"}`, v); err == nil || errors.Is(err, ErrNoJSON) {
			t.Errorf("Decode into %T: err = %v, want a usage error", v, err)
		}
	}
}

// The whole text is one value with trailing prose: the whole-text attempt
// must refuse it (trailing data) and the candidate must succeed.
func TestDecodeTrailingDataGoesToTheCandidate(t *testing.T) {
	var got map[string]int
	if err := Decode(`{"a":1} and also {"b":2}`, &got); err != nil || got["a"] != 1 || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCandidates(t *testing.T) {
	text := "a {\"x\":{\"y\":[1,{\"z\":2}]}} b [3,4] c {unclosed d \"{\" {\"s\":\"]\"}"
	got := Candidates(text)
	want := []string{`{"x":{"y":[1,{"z":2}]}}`, `[3,4]`, `{"s":"]"}`}
	// "{unclosed ..." never closes at top level: it swallows to the end and is
	// not a candidate; the scan then finds the balanced value inside it.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Candidates = %q\nwant %q", got, want)
	}
	if got := Candidates("no brackets at all"); got != nil {
		t.Errorf("no brackets: %q", got)
	}
}

func TestFirst(t *testing.T) {
	if got, ok := First("x {bad} [1,2] {\"a\":1}"); !ok || got != "[1,2]" {
		t.Errorf("First = %q, %v", got, ok)
	}
	if _, ok := First("{bad} {also: bad}"); ok {
		t.Error("no valid candidate must be false")
	}
}

// FuzzCandidates: arbitrary text never panics, every candidate is a substring
// that starts and ends with a matching bracket pair, and Decode never panics.
func FuzzCandidates(f *testing.F) {
	for _, s := range []string{"", "{", "}", "{\"a\":\"}\"}", "[[[]]", "x {\"a\":[1,2]} y", "\"\\\"{", "{\"a\":\"\\\\\"}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, c := range Candidates(s) {
			if !strings.Contains(s, c) || len(c) < 2 {
				t.Fatalf("candidate %q is not a substring of the input", c)
			}
			if !(c[0] == '{' && c[len(c)-1] == '}') && !(c[0] == '[' && c[len(c)-1] == ']') {
				t.Fatalf("candidate %q does not open and close with a matching pair", c)
			}
		}
		var v any
		_ = Decode(s, &v)
	})
}
