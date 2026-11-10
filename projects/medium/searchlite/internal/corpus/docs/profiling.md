# Profiling

The pprof tool samples a running program to show where its time and memory go. A CPU profile reports which functions were on the stack most often, while a heap profile shows which call sites allocate and retain memory. Import net/http/pprof to expose profiles on a live server, then fetch one with go tool pprof. The flame graph view makes hot paths obvious at a glance. A goroutine profile is the quickest way to find leaks, since it lists every stack that is still alive. The execution tracer complements pprof by showing scheduling delays and garbage collection pauses over time.
