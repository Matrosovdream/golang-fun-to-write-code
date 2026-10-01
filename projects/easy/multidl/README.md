# E15 multidl — concurrent downloader

`multidl -c 4 urls.txt` — all URLs at once, but never more than `-c` in flight.

The bridge project: this is `fetchpool` (medium) reduced to its skeleton. Master this shape,
then open fetchpool and watch the same bones carry errgroup, rate limiting and a crawler.

## What it teaches

- **The fan-out/collect shape**, piece by piece:
  - one goroutine per URL + `sync.WaitGroup`;
  - a **buffered channel as a semaphore** (`sem <- struct{}{}` / `<-sem`) bounding concurrency;
  - results returned over a channel — *no shared slice, no mutex*;
  - the closer goroutine (`wg.Wait(); close(ch)`) that turns completion into a closed
    channel, so collection is a plain `range`.
- **`errors.Join`**: every failure in one error, `nil` when none — made for summaries.
- The semaphore **test**: a slow server + atomic high-water mark proving the bound holds.
- `f.Close()` error checked on writes — an unflushed download is a failed download.
- Deriving filenames from URLs (`net/url` + `path.Base`), file-or-args input handling.

## Run

```sh
go test -race ./...
printf 'https://go.dev/\nhttps://example.com/\n' > /tmp/urls.txt
go run . -c 2 -dir /tmp/dl /tmp/urls.txt
```

## Rewrite exercises

1. Rebuild `downloadAll` from the four-bullet shape above, tests first.
2. Forget the closer goroutine on purpose — observe the deadlock, explain it, fix it.
3. Add a live progress line: completed/total updating via a third channel.
4. Then rewrite it once more with `errgroup.SetLimit` (see fetchpool) and compare line counts.
