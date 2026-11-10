# The race detector

A data race happens when two goroutines touch the same memory, at least one of them writes, and nothing orders the accesses. The race detector instruments memory operations at build time and reports any pair that the memory model leaves unordered. Enable it with the -race flag on go test, go run or go build. It only finds races that actually execute, so exercise your concurrent code paths in tests. Expect extra memory use and a slower run, which is fine for tests but rarely for production. A clean run is evidence, not proof.
