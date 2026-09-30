package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestWantsColors(t *testing.T) {
	cases := map[string]bool{
		"":        true,
		"s":       true,
		"S":       true,
		"sim":     true,
		"SIM":     true,
		"y":       true,
		"n":       false,
		"N":       false,
		"nao":     false,
		"NAO":     false,
		"não":     false,
		"Não":     false,
		"NÃO":     false,
		"x\nsim":  true,
		"x\nnão":  false,
		"?\n?\nn": false,
	}
	for answer, want := range cases {
		colorsEnabled = true
		got := wantsColors(bufio.NewScanner(strings.NewReader(answer+"\n")), true)
		if got != want {
			t.Errorf("answer %q: got %v want %v", answer, got, want)
		}
	}
}

func TestWantsColorsSkipsQuestionWhenNotInteractive(t *testing.T) {
	colorsEnabled = true
	in := bufio.NewScanner(strings.NewReader("n\nopen 1\n"))
	if !wantsColors(in, false) {
		t.Fatal("non-interactive input must keep colors and not consume the answer")
	}
	if !in.Scan() || in.Text() != "n" {
		t.Error("first input line was consumed")
	}
}

func TestWantsColorsRespectsNoColor(t *testing.T) {
	colorsEnabled = false
	if wantsColors(bufio.NewScanner(strings.NewReader("s\n")), true) {
		t.Fatal("NO_COLOR must win over the answer")
	}
	colorsEnabled = true
}

func TestStatusLineColors(t *testing.T) {
	colorsEnabled = true
	if !strings.Contains(statusLine(200), "\033[32m") || !strings.Contains(statusLine(422), "\033[31m") {
		t.Error("2xx must be green and 4xx red")
	}
	colorsEnabled = false
	if statusLine(200) != "HTTP 200" {
		t.Error("colors disabled must print plain text")
	}
	colorsEnabled = true
}
