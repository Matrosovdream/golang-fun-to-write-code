// Package web embeds the browser client.
package web

import "embed"

//go:embed index.html rga.js
var Files embed.FS
