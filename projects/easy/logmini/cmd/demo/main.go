// A demo app consuming the logmini package — including struct embedding:
// Server embeds *logmini.Logger, so s.Infof works directly.
package main

import (
	"os"

	"logmini"
)

type Server struct {
	*logmini.Logger // embedded: its methods are promoted onto Server
	addr string
}

func main() {
	log := logmini.New(os.Stdout, logmini.Debug)

	log.Debugf("config loaded from %s", "env")
	log.Infof("starting up")

	s := &Server{
		Logger: log.WithPrefix("server: "),
		addr:   ":8080",
	}
	s.Infof("listening on %s", s.addr) // promoted method, prefixed output
	s.Warnf("low disk space: %dMB left", 512)

	// The package-level default logger (stderr), like stdlib log.Printf:
	logmini.Errorf("this one goes to stderr via the default logger")
}
