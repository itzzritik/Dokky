//go:build !darwin

package main

func macVersion() (string, error) { return "", nil }
