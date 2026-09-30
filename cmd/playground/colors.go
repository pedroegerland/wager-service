package main

import (
	"fmt"
	"os"
)

var colorsEnabled = os.Getenv("NO_COLOR") == ""

func paint(code, s string) string {
	if !colorsEnabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func green(s string) string  { return paint("32", s) }
func red(s string) string    { return paint("31", s) }
func yellow(s string) string { return paint("33", s) }

func statusLine(code int) string {
	line := fmt.Sprintf("HTTP %d", code)
	if code >= 200 && code < 300 {
		return green(line)
	}
	return red(line)
}

func okOrFail(ok bool, s string) string {
	if ok {
		return green(s)
	}
	return red(s)
}
