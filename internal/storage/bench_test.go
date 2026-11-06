package storage

import (
	"fmt"
	"testing"
)

func BenchmarkStoragePerformance(b *testing.B) {
	s := GPageStorage

	pids := make([]string, 36)
	for i := 0; i < len(pids); i++ {
		pid := fmt.Sprintf("bench-page-%d", i)
		_, _ = s.CreatePage(pid, "initial content")
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
