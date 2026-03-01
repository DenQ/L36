package storage

import (
	"fmt"
	"testing"
)

// Тест на "прочность" и чистый RPS
func BenchmarkStoragePerformance(b *testing.B) {
	s := GPageStorage

	pids := make([]string, 36)
	symbols := "0123456789abcdefghijklmnopqrstuvwxyz"
	for i, char := range symbols {
		pid := string(char) + "-bench-page"
		s.CreatePage(pid, "initial content")
		pids[i] = pid
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			pid := pids[i%36]
			s.AddVersion(pid, fmt.Sprintf("updated content %d", i))
			i++
		}
	})
}

// ➜  L36 git:(master) go test -bench=BenchmarkStoragePerformance -benchmem ./internal/storage

// [L-36] Restored version 1 from 1000 patches in 553.554µs
// goos: darwin
// goarch: amd64
// pkg: l36/internal/storage
// cpu: Intel(R) Core(TM) i9-9880H CPU @ 2.30GHz
// BenchmarkStoragePerformance-16    	  364944	      3539 ns/op	    1548 B/op	      30 allocs/op
// PASS
// ok  	l36/internal/storage	2.161s

// ➜  L36 git:(master) ✗ go test -bench=BenchmarkStoragePerformance -benchmem ./internal/storage

// [L-36] Restored version 1 from 1000 patches in 494.246µs
// goos: darwin
// goarch: amd64
// pkg: l36/internal/storage
// cpu: Intel(R) Core(TM) i9-9880H CPU @ 2.30GHz
// BenchmarkStoragePerformance-16    	  796767	      1610 ns/op	    1473 B/op	      28 allocs/op
// PASS
// ok  	l36/internal/storage	1.830s
