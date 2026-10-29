// SPDX-License-Identifier: BSD-3-Clause

package toml_test

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/org-runink/runi/toml"
)

func ExampleUnmarshal() {
	doc := []byte(`
[[models]]
name = "chat"
temperature = 0.7
stop = ["</s>"]

[[models]]
name = "coder"
temperature = 0.2
top_k = 20
`)
	var cfg struct {
		Models []struct {
			Name        string   `toml:"name"`
			Temperature float64  `toml:"temperature"`
			TopK        int      `toml:"top_k"`
			Stop        []string `toml:"stop"`
		} `toml:"models"`
	}
	if err := toml.Unmarshal(doc, &cfg); err != nil {
		fmt.Println(err)
		return
	}
	for _, m := range cfg.Models {
		fmt.Println(m.Name, m.Temperature, m.TopK, m.Stop)
	}
	// Output:
	// chat 0.7 0 [</s>]
	// coder 0.2 20 []
}

func ExampleParse() {
	doc, err := toml.Parse([]byte(`
title = "sampling"
limits = { context = 8_192, batch = 8 }
when = 2026-10-29
`))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%v %T\n", doc["title"], doc["title"])
	fmt.Printf("%v %T\n", doc["limits"], doc["limits"].(map[string]any)["context"])
	fmt.Printf("%v %T\n", doc["when"], doc["when"])
	// Output:
	// sampling string
	// map[batch:8 context:8192] int64
	// 2026-10-29 toml.LocalDate
}

func ExampleDecoder_DisallowUnknownFields() {
	var cfg struct {
		Port int `toml:"port"`
	}
	d := toml.NewDecoder(strings.NewReader("port = 8080\nprot = 9090\n"))
	d.DisallowUnknownFields()
	fmt.Println(d.Decode(&cfg))
	// Output: toml: prot: unknown key: the struct has no field for it (line 2:1)
}

func ExampleError() {
	var cfg struct {
		Servers []struct {
			Port int `toml:"port"`
		} `toml:"servers"`
	}
	err := toml.Unmarshal([]byte("[[servers]]\nport = 80\n[[servers]]\nport = \"443\"\n"), &cfg)
	fmt.Println(err)
	// Output: toml: servers[1].port: expected integer, got string (line 4:1)
}

// Frontmatter in +++ fences is TOML ahead of the body of a document.
func ExampleUnmarshal_frontmatter() {
	file := []byte("+++\nname = \"summarise\"\ntools = [\"read\", \"search\"]\n+++\nSummarise the thread in three lines.\n")

	rest, ok := bytes.CutPrefix(file, []byte("+++\n"))
	if !ok {
		fmt.Println("no frontmatter")
		return
	}
	meta, body, ok := bytes.Cut(rest, []byte("\n+++\n"))
	if !ok {
		fmt.Println("frontmatter is never closed")
		return
	}
	var fm struct {
		Name  string   `toml:"name"`
		Tools []string `toml:"tools"`
	}
	if err := toml.Unmarshal(meta, &fm); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(fm.Name, fm.Tools)
	fmt.Print(string(body))
	// Output:
	// summarise [read search]
	// Summarise the thread in three lines.
}
