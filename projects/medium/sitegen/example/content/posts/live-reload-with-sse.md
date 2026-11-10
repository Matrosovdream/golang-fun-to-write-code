---
title: Live reload with SSE
date: 2026-03-16
tags: [web, go]
summary: One long-lived HTTP response is all a browser needs to hear about a rebuild.
---
The dev server injects a tiny script that opens an `EventSource`.
When a rebuild finishes, the server writes `event: reload` to every open
response and the page calls `location.reload()`.

- [x] flush after every event
- [x] stop when the request context is cancelled
- [ ] websockets (not needed)
