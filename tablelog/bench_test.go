// SPDX-License-Identifier: BSD-3-Clause

package tablelog

import (
	"context"
	"fmt"
	"testing"
)

// The restbench workload, in-package so `go test -bench` reproduces it:
// 10,000 rows in 100 commits of 100, then one full scan.
const (
	benchRows  = 10000
	benchBatch = 100
)

func benchFill(b *testing.B, tb *Table) {
	b.Helper()
	ctx := context.Background()
	batch := make([]Row, 0, benchBatch)
	for i := 0; i < benchRows; i++ {
		batch = append(batch, Row{
			Key:     fmt.Sprintf("k%06d", i),
			Payload: []byte(fmt.Sprintf(`{"event":%d,"kind":"write"}`, i)),
		})
		if len(batch) == benchBatch {
			if _, err := tb.Put(ctx, batch...); err != nil {
				b.Fatal(err)
			}
			batch = batch[:0]
		}
	}
}

// BenchmarkWrite is restbench's tablelog_write_s: 10,000 rows in 100 commits.
func BenchmarkWrite(b *testing.B) {
	for b.Loop() {
		tb, err := Open(NewMemStore(), "bench", "events")
		if err != nil {
			b.Fatal(err)
		}
		benchFill(b, tb)
	}
}

// BenchmarkRead is restbench's tablelog_read_s: scan 10,000 rows back out of
// the 100 files the write left behind.
func BenchmarkRead(b *testing.B) {
	tb, err := Open(NewMemStore(), "bench", "events")
	if err != nil {
		b.Fatal(err)
	}
	benchFill(b, tb)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		rs, err := tb.Scan(ctx, "")
		if err != nil {
			b.Fatal(err)
		}
		if len(rs) != benchRows {
			b.Fatalf("scanned %d rows, want %d", len(rs), benchRows)
		}
	}
}
