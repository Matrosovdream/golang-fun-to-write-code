package hub

import "time"

// Envelope is the single wire format in both directions; Type decides which
// fields matter. One struct instead of an interface keeps JSON handling flat.
type Envelope struct {
	Type string `json:"type"` // client→server: join, msg, nick — server→client: msg, info, history, error
	Room string `json:"room,omitempty"`
	From string `json:"from,omitempty"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`

	Messages []Envelope `json:"messages,omitempty"` // only for type=history
	At       time.Time  `json:"at,omitempty"`
}

func info(room, text string) Envelope {
	return Envelope{Type: "info", Room: room, Text: text, At: time.Now()}
}

func errMsg(text string) Envelope {
	return Envelope{Type: "error", Text: text}
}
