# Mutexes

A sync.Mutex protects a critical section: only the goroutine holding the lock may touch the data it guards. Pair every Lock with a deferred Unlock so that early returns and panics still release it. A RWMutex allows many concurrent readers or a single writer, which pays off only when reads vastly outnumber writes. Keep the guarded fields next to the mutex in the same struct, and never copy a mutex after first use. Hold locks for as short a time as possible, and never call into unknown code while holding one, since that is how deadlocks begin.
