// A terminal chat client:
//
//	go run ./cmd/client -nick stan -room go
//	> hello everyone
//	> /join random
//	> /nick stanley
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"

	"chathub/internal/hub"
)

func main() {
	var (
		server = flag.String("server", "localhost:8086", "server host:port")
		nick   = flag.String("nick", "anon", "nickname")
		room   = flag.String("room", "general", "room to join")
	)
	flag.Parse()

	u := url.URL{
		Scheme:   "ws",
		Host:     *server,
		Path:     "/ws",
		RawQuery: url.Values{"nick": {*nick}, "room": {*room}}.Encode(),
	}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Receiver goroutine: prints everything the server pushes.
	go func() {
		for {
			var env hub.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				fmt.Println("\n* connection closed:", err)
				os.Exit(0)
			}
			printEnvelope(env)
		}
	}()

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		var env hub.Envelope
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "/join "):
			env = hub.Envelope{Type: "join", Room: strings.TrimPrefix(line, "/join ")}
		case strings.HasPrefix(line, "/nick "):
			env = hub.Envelope{Type: "nick", Name: strings.TrimPrefix(line, "/nick ")}
		default:
			env = hub.Envelope{Type: "msg", Text: line}
		}
		if err := conn.WriteJSON(env); err != nil {
			fmt.Fprintln(os.Stderr, "send:", err)
			return
		}
	}
}

func printEnvelope(env hub.Envelope) {
	switch env.Type {
	case "msg":
		fmt.Printf("[%s] %s: %s\n", env.Room, env.From, env.Text)
	case "info":
		fmt.Printf("* %s\n", env.Text)
	case "history":
		for _, m := range env.Messages {
			fmt.Printf("[%s] %s: %s (history)\n", m.Room, m.From, m.Text)
		}
	case "error":
		fmt.Printf("! %s\n", env.Text)
	}
}
