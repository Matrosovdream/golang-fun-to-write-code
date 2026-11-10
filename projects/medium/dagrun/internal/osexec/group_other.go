//go:build !unix

package osexec

import "os/exec"

// Without process groups only the shell itself is killed on cancellation.
func ownGroup(*exec.Cmd) {}

func killGroup(*exec.Cmd) {}
