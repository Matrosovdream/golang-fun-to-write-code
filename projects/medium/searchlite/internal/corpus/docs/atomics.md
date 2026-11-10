# Atomic operations

The sync/atomic package offers indivisible loads, stores and compare-and-swap on integers and pointers. Atomics are cheaper than a mutex for a single counter or flag, but they only protect one word at a time. The memory model treats atomic operations as synchronizing, so a value stored through atomic.Int64 is visible to a goroutine that later loads it. Reach for atomics to publish a configuration pointer or count requests; reach for a mutex when several fields must change together. Lock-free code is easy to get subtly wrong.
