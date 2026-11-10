# Goroutines

A goroutine is a function running concurrently with the rest of your program, started with the `go` keyword. Goroutines are far cheaper than operating system threads: each begins with a stack of a few kilobytes that grows and shrinks on demand. The runtime scheduler multiplexes thousands of goroutines onto a small pool of threads, parking one whenever it blocks on a channel, a lock or a system call. Starting a goroutine is easy; knowing when it will stop is the hard part. Every goroutine you launch should have a clear owner and a clear way to finish, otherwise it leaks.
