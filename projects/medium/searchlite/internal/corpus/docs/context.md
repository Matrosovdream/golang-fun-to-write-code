# Context and cancellation

A context carries a deadline, a cancellation signal and a few request-scoped values across API boundaries. Functions that block or do slow work should accept a context as their first parameter and return promptly when it is done. Derive a child context with a timeout and always call the returned cancel function, even on the success path, to release its resources. Cancellation is cooperative: nothing stops a goroutine that never checks. Do not store contexts in structs; pass them along the call chain instead.
