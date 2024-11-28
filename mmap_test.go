package log

import (
	"testing"
)

// 原来日志打印方式
func Benchmark_NomalLog(b *testing.B) {
	b.ResetTimer()
	log := New(&Config{Output: OutputFile, Filename: "./log/normal.log"})
	for i := 0; i < b.N; i++ {
		log.Infof("testsdafougdsaljgdaljgdladgjlsadgjlagdladgljkadgljagdljkladjgadljksgljkasgdjlgjlkagldjljgkd")
	}
	b.StopTimer()
}

// mmap日志打印方式
func Benchmark_MmapLog(b *testing.B) {
	b.ResetTimer()
	log := New(&Config{Output: OutputMmap, Filename: "./log/mmap.log"})
	for i := 0; i < b.N; i++ {
		log.Infof("testsdafougdsaljgdaljgdladgjlsadgjlagdladgljkadgljagdljkladjgadljksgljkasgdjlgjlkagldjljgkd")
	}
	b.StopTimer()
	mmapLogger.StopMmapLogger()
}
