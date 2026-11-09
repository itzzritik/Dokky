//go:build !windows

package sysauth

import "os/exec"

func prepare(*exec.Cmd) {}

func started(command, *exec.Cmd) func() { return func() {} }
