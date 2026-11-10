// Package wire is the JSON protocol spoken over the WebSocket.
//
//	client → server  hello  {replica, version}   first message, once per connection
//	server → client  sync   {version, ops}       what the client lacks + the server's version
//	either way       ops    {ops}                live edits
//
// After sync the client sends whatever the server's version says it lacks
// (offline edits), then both sides just stream ops.
package wire

import "crdtpad/internal/rga"

const (
	Hello = "hello"
	Sync  = "sync"
	Ops   = "ops"
)

// Message is one struct for every message type, as in chathub: simpler on
// the wire than a type hierarchy, at the cost of fields that only some types use.
type Message struct {
	Type    string      `json:"type"`
	Replica string      `json:"replica,omitempty"`
	Version rga.Version `json:"version,omitempty"`
	Ops     []rga.Op    `json:"ops,omitempty"`
}
