package main

import "golang.org/x/sys/unix"

func macVersion() (string, error) { return unix.Sysctl("kern.osproductversion") }
