# Testing

Tests are ordinary functions named TestXxx in files ending in _test.go, run with go test. Table-driven tests list inputs and expected outputs in a slice and loop over it with a subtest for each row, so a failure names the exact case. Use t.Helper in assertion helpers, t.Cleanup for teardown and t.TempDir for scratch files. Fuzzing generates inputs automatically and saves any crashing input as a regression seed. Run the whole suite with the race detector before you trust any concurrent code.
