package lazy

import (
	"context"
	"testing"
)

func BenchmarkGetResolved(b *testing.B) {
	v := Of(42)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = v.Get(ctx)
	}
}

func BenchmarkGetResolvedParallel(b *testing.B) {
	v := Of(42)
	ctx := context.Background()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = v.Get(ctx)
		}
	})
}

func BenchmarkNewAndResolve(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v := New(func(context.Context) (int, error) { return 1, nil })
		_, _ = v.Get(ctx)
	}
}

func BenchmarkMapChain(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		src := New(func(context.Context) (int, error) { return 1, nil })
		v := Map(Map(Map(src, func(i int) int { return i + 1 }), func(i int) int { return i * 2 }), func(i int) int { return i - 3 })
		_, _ = v.Get(ctx)
	}
}
