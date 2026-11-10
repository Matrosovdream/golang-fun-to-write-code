# Garbage collection

Go reclaims memory with a concurrent, tri-color mark and sweep collector that runs alongside your goroutines. It aims for short pauses rather than maximum throughput. The GOGC setting controls how much the heap may grow between cycles, and GOMEMLIMIT adds a soft ceiling for container deployments. Fewer allocations mean less work for the collector, so reusing buffers with sync.Pool can help hot paths. Escape analysis keeps values on the stack when it can prove they do not outlive the function.
