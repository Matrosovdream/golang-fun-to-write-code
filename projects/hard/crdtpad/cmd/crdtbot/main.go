// Command crdtbot is the demo client: K bots edit one document at random
// against a live server, some of them going offline mid-run, and at the end
// every bot (and the server) must hold the identical text.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"os"
	"sync"
	"time"

	"crdtpad/internal/client"
	"crdtpad/internal/rga"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz "

type bot struct {
	name     string
	c        *client.Client
	rng      *rand.Rand
	offlines int
}

func main() {
	base := flag.String("server", "http://127.0.0.1:8106", "crdtpad base URL")
	doc := flag.String("doc", fmt.Sprintf("bots-%d", time.Now().Unix()), "document name (fresh by default)")
	n := flag.Int("bots", 4, "number of concurrent bots")
	dur := flag.Duration("duration", 4*time.Second, "how long the bots edit")
	seed := flag.Uint64("seed", 1, "seed for the bots' edit choices")
	flag.Parse()

	ok, err := run(*base, *doc, *n, *dur, *seed)
	if err != nil {
		fmt.Fprintln(os.Stderr, "crdtbot:", err)
		os.Exit(1)
	}
	if !ok {
		os.Exit(2)
	}
}

func run(base, doc string, n int, dur time.Duration, seed uint64) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmt.Printf("%d bots editing %q on %s for %v\n", n, doc, base, dur)

	bots := make([]*bot, n)
	for i := range bots {
		name := fmt.Sprintf("bot%d-%04x", i, rand.Uint32N(1<<16)) // replica names must never repeat
		b := &bot{name: name, c: client.New(name, base, doc), rng: rand.New(rand.NewPCG(seed, uint64(i)))}
		if err := b.c.Connect(ctx); err != nil {
			return false, err
		}
		bots[i] = b
	}

	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup
	for _, b := range bots {
		wg.Go(func() { b.edit(ctx, deadline) })
	}
	wg.Wait()

	want := rga.Version{}
	total := uint64(0)
	for _, b := range bots {
		want[b.name] = b.c.Version()[b.name]
		total += want[b.name]
	}
	deadline = time.Now().Add(15 * time.Second)
	for !settled(bots, want) {
		if time.Now().After(deadline) {
			return false, fmt.Errorf("bots did not receive all %d ops within 15s", total)
		}
		time.Sleep(20 * time.Millisecond)
	}

	serverText, err := fetch(ctx, base+"/doc/"+doc)
	if err != nil {
		return false, err
	}
	converged := true
	offlines := 0
	for _, b := range bots {
		text := b.c.Text()
		same := text == serverText
		converged = converged && same
		offlines += b.offlines
		fmt.Printf("  %-10s ops=%-4d offline episodes=%d  chars=%d  matches server: %v\n", b.name, want[b.name], b.offlines, len([]rune(text)), same)
	}
	fmt.Printf("%d ops from %d bots (%d offline episodes)\n", total, n, offlines)
	fmt.Printf("converged: %v  (server holds %d characters)\n", converged, len([]rune(serverText)))
	fmt.Printf("document starts: %q\n", clip(serverText, 70))
	return converged, nil
}

// edit types and deletes at random positions; now and then the bot drops its
// connection, keeps editing blind for a few edits, and reconnects.
func (b *bot) edit(ctx context.Context, deadline time.Time) {
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(5+b.rng.IntN(20)) * time.Millisecond)
		if b.rng.IntN(100) < 3 {
			b.c.Disconnect()
			b.offlines++
			for range 10 {
				b.once()
			}
			if err := b.c.Connect(ctx); err != nil {
				fmt.Fprintln(os.Stderr, b.name, "reconnect:", err)
				return
			}
			continue
		}
		b.once()
	}
}

// once makes one random edit. A remote delete can shrink the document between
// Len and the edit; the resulting out-of-range error just skips this edit.
func (b *bot) once() {
	n := b.c.Len()
	if n > 0 && b.rng.IntN(100) < 30 {
		b.c.Delete(b.rng.IntN(n))
		return
	}
	b.c.Insert(b.rng.IntN(n+1), rune(alphabet[b.rng.IntN(len(alphabet))]))
}

// settled: every bot holds every bot's ops (and any foreign replica's, if a
// human is typing in a browser on the same document) and they all agree.
func settled(bots []*bot, want rga.Version) bool {
	ref := bots[0].c.Version()
	for name, seq := range want {
		if ref[name] < seq {
			return false
		}
	}
	for _, b := range bots[1:] {
		if !maps.Equal(b.c.Version(), ref) {
			return false
		}
	}
	return true
}

func fetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
