# Errors

Errors in Go are ordinary values returned alongside results. Wrap an error with fmt.Errorf and the %w verb to add context while keeping the original available. Callers inspect the chain with errors.Is for sentinel values and errors.As for typed errors. Define a custom error type when callers need structured detail, such as the byte offset of a syntax mistake. Handle an error once: either log it or return it, not both. Joining several errors with errors.Join suits cleanup code that must report every failure.
