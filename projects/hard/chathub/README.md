# chathub — real-time WebSocket chat

Multi-room chat server + terminal client. Rooms, nicknames, presence events, history replay.

## What this project teaches

- **The hub pattern** — the canonical Go architecture for long-lived connections:
  ALL shared state lives in one `Run` goroutine; read/write pumps only pass messages.
  Zero mutexes, race-free by construction (`go test -race` proves it).
- **Two pumps per connection**: gorilla/websocket allows one reader and one writer —
  `readPump` + `writePump` with a buffered `send` channel between hub and socket.
- **Slow consumers**: `trySend` uses `select`/`default` — a client whose buffer is full is
  disconnected rather than allowed to stall the entire hub. Read the comment there; this
  decision (drop message? block? drop client?) is *the* design question of every pub/sub.
- **Keepalive done right**: ping ticker + pong handler pushing the read deadline; why the
  HTTP server must NOT set Read/WriteTimeout for WebSocket endpoints.
- **Request/response over channels**: `Stats()` shows how outside goroutines query
  loop-owned state safely.
- **Graceful drain**: shutdown broadcasts a notice, closes every `send` channel, and the
  write pumps emit proper close frames.
- **Ring buffer** for per-room history — fixed memory, O(1) append.
- **Single wire struct** (`Envelope`) vs a type hierarchy — the pragmatic JSON protocol choice.

## Run

```sh
go test -race ./...

go run ./cmd/server
# terminal 2:
go run ./cmd/client -nick alice -room go
# terminal 3:
go run ./cmd/client -nick bob -room go
# type messages; /join other-room, /nick newname
curl -s localhost:8086/rooms   # room stats
```

## Rewrite exercises

1. Rebuild the hub loop from scratch; keep `go test -race ./...` green.
2. Swap the "drop slow client" policy for "drop oldest message" — what breaks in ordering?
3. Add direct messages (`/dm bob hi`) without breaking the one-owner-goroutine rule.
4. Add a per-client token-bucket rate limit inside the hub (flooding protection).
5. Port it from gorilla to `coder/websocket` and compare the APIs (ctx-based, no pumps needed).
