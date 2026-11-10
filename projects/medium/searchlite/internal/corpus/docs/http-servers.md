# HTTP servers

The net/http package runs each incoming request in its own goroutine, so handlers must be safe for concurrent use. Set timeouts on the server, because the defaults wait forever on slow clients. A ServeMux in recent Go versions matches on method and path patterns such as GET /items/{id}. Shut down gracefully by calling Shutdown with a context, which stops accepting connections and waits for active requests to finish. Wrap handlers in middleware functions for logging, authentication and panic recovery.
