# WaitGroups and errgroup

A sync.WaitGroup lets one goroutine wait for a collection of others to finish. Call Add before starting the goroutine, never inside it, or the waiter may return too early. Recent Go versions offer a Go method that does this bookkeeping for you. The errgroup package goes further: it collects the first error from a set of goroutines and cancels a shared context when one of them fails. Setting a limit on the group turns it into a simple bounded worker pool.
