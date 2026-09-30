// loggen writes a synthetic access log to stdout — pipe it to a file:
//
//	go run ./cmd/loggen -lines 1000000 > /tmp/access.log
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
)

var (
	paths = []string{"/", "/api/users", "/api/items", "/login", "/static/app.js",
		"/api/orders", "/health", "/search", "/api/users/42", "/img/logo.png"}
	methods  = []string{"GET", "GET", "GET", "GET", "POST", "PUT", "DELETE"}
	statuses = []int{200, 200, 200, 200, 200, 200, 301, 400, 401, 404, 404, 500, 503}
)

func main() {
	lines := flag.Int("lines", 100_000, "number of lines to generate")
	flag.Parse()

	// Unbuffered writes to stdout would syscall per line; bufio batches them.
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer w.Flush()

	// One reused buffer + strconv.Append* instead of fmt.Sprintf per line:
	// generation runs allocation-free in the loop.
	buf := make([]byte, 0, 256)
	for range *lines {
		buf = buf[:0]
		buf = append(buf, "10.0."...)
		buf = strconv.AppendInt(buf, rand.Int64N(32), 10)
		buf = append(buf, '.')
		buf = strconv.AppendInt(buf, rand.Int64N(256), 10)
		buf = append(buf, ` [30/Sep/2026:12:00:00] "`...)
		buf = append(buf, methods[rand.IntN(len(methods))]...)
		buf = append(buf, ' ')
		buf = append(buf, paths[rand.IntN(len(paths))]...)
		buf = append(buf, `" `...)
		buf = strconv.AppendInt(buf, int64(statuses[rand.IntN(len(statuses))]), 10)
		buf = append(buf, ' ')
		buf = strconv.AppendInt(buf, rand.Int64N(50_000), 10)
		buf = append(buf, ' ')
		buf = strconv.AppendFloat(buf, rand.Float64()*250, 'f', 3, 64)
		buf = append(buf, '\n')
		if _, err := w.Write(buf); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
