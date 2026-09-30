// loadgen hammers metricsd over N TCP connections and reports throughput.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		addr     = flag.String("addr", "localhost:9125", "metricsd ingest address")
		conns    = flag.Int("conns", 4, "parallel connections")
		duration = flag.Duration("duration", 3*time.Second, "how long to blast")
	)
	flag.Parse()

	names := make([][]byte, 64)
	for i := range names {
		names[i] = []byte(fmt.Sprintf("app_metric_%d", i))
	}

	var sent atomic.Int64
	deadline := time.Now().Add(*duration)

	var wg sync.WaitGroup
	for range *conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", *addr)
			if err != nil {
				fmt.Fprintln(os.Stderr, "dial:", err)
				return
			}
			defer conn.Close()

			// The sender mirrors the server's discipline: buffered writer,
			// one reused line buffer, strconv.Append — no allocs per line.
			w := bufio.NewWriterSize(conn, 64*1024)
			defer w.Flush()
			buf := make([]byte, 0, 64)
			n := 0
			for time.Now().Before(deadline) {
				for range 1000 { // check the clock once per batch, not per line
					buf = buf[:0]
					switch n % 10 {
					case 0:
						buf = append(buf, "g queue_depth "...)
						buf = strconv.AppendInt(buf, rand.Int64N(100), 10)
					case 1, 2, 3:
						buf = append(buf, "h latency_ms "...)
						buf = strconv.AppendInt(buf, rand.Int64N(2000), 10)
					default:
						buf = append(buf, 'c', ' ')
						buf = append(buf, names[rand.IntN(len(names))]...)
						buf = append(buf, " 1"...)
					}
					buf = append(buf, '\n')
					if _, err := w.Write(buf); err != nil {
						return
					}
					n++
				}
				sent.Add(1000)
			}
		}()
	}
	wg.Wait()

	total := sent.Load()
	fmt.Printf("sent %d events over %d conns in %s — %.2fM events/s\n",
		total, *conns, *duration, float64(total)/duration.Seconds()/1e6)
}
