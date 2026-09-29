package customers

import (
	"strings"
	"unicode"
)

func NormalizePhone(rawPhone string) (string, bool) {
	phoneDigits := digitsOf(rawPhone)
	switch {
	case strings.HasPrefix(phoneDigits, "255") && len(phoneDigits) == 12:
		phoneDigits = "0" + phoneDigits[3:]
	case len(phoneDigits) == 9 && !strings.HasPrefix(phoneDigits, "0"):
		phoneDigits = "0" + phoneDigits
	}

	isTanzanianNumber := len(phoneDigits) == 10 && strings.HasPrefix(phoneDigits, "0") && phoneDigits[1] != '0'
	return phoneDigits, isTanzanianNumber
}

func phoneSearchFragment(searchText string) string {
	searchDigits := digitsOf(searchText)
	if strings.HasPrefix(searchDigits, "255") {
		searchDigits = "0" + searchDigits[3:]
	}
	if len(searchDigits) < 3 {
		return ""
	}
	return searchDigits
}

func digitsOf(text string) string {
	digitsOnly := strings.Builder{}
	for _, character := range text {
		if unicode.IsDigit(character) && character < unicode.MaxASCII {
			digitsOnly.WriteRune(character)
		}
	}
	return digitsOnly.String()
}
