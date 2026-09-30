package main

import "regexp"

var (
	wholeAmount     = regexp.MustCompile(`^[0-9]+$`)
	oneDecimalPlace = regexp.MustCompile(`^[0-9]+\.[0-9]$`)
	commaDecimal    = regexp.MustCompile(`^[0-9]+,[0-9]{1,2}$`)
)

func normalizeAmount(s string) string {
	if commaDecimal.MatchString(s) {
		s = regexp.MustCompile(`,`).ReplaceAllString(s, ".")
	}
	switch {
	case wholeAmount.MatchString(s):
		return s + ".00"
	case oneDecimalPlace.MatchString(s):
		return s + "0"
	}
	return s
}
