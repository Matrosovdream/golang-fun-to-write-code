# Benchmarks

A benchmark is a function named BenchmarkXxx that loops b.N times while the framework adjusts N until the timing is stable. Run it with go test -bench and add -benchmem to see allocations per operation. Move expensive setup before b.ResetTimer so it does not pollute the result. Compare two runs with benchstat, since a single run on a busy laptop is mostly noise. Measure first and optimize second; the slow part is rarely where you guessed.
