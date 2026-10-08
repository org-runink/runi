package memo

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func BenchmarkDoHit(b *testing.B) {
	c := New[string, int](Options{Capacity: 1024})
	fn := func(context.Context) (int, error) { return 1, nil }
	ctx := context.Background()
	_, _ = c.Do(ctx, "k", fn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Do(ctx, "k", fn)
	}
}

func BenchmarkDoMiss(b *testing.B) {
	c := New[int, int](Options{})
	fn := func(context.Context) (int, error) { return 1, nil }
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Do(ctx, i, fn)
	}
}

func BenchmarkDoHitParallel(b *testing.B) {
	c := New[string, int](Options{Capacity: 1024})
	fn := func(context.Context) (int, error) { return 1, nil }
	ctx := context.Background()
	_, _ = c.Do(ctx, "k", fn)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Do(ctx, "k", fn)
		}
	})
}

func BenchmarkHash(b *testing.B) {
	m := map[string]any{"query": "what is the capital of France", "lang": "en", "top_k": 5, "temp": 0.2}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Hash("model-v3", m)
	}
}

// BenchmarkStampede measures the whole point of single-flight: N goroutines
// arriving together on a cold key. Without coalescing this is N executions.
func BenchmarkStampede(b *testing.B) {
	for _, n := range []int{8, 64} {
		b.Run(fmt.Sprintf("callers=%d", n), func(b *testing.B) {
			ctx := context.Background()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c := New[int, int](Options{})
				var wg sync.WaitGroup
				for j := 0; j < n; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, _ = c.Do(ctx, 0, func(context.Context) (int, error) { return 1, nil })
					}()
				}
				wg.Wait()
			}
		})
	}
}
