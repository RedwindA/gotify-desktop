//go:build !windows && !darwin && !linux

package appearance

func readAccent() string { return "" }
