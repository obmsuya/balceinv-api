package response

import "testing"

func TestSentenceCase(t *testing.T) {
	cases := map[string]string{
		"nothing was imported": "Nothing was imported",
		"SKU taken":            "SKU taken",
		"éclair missing":       "Éclair missing",
		"":                     "",
	}
	for input, want := range cases {
		if got := sentenceCase(input); got != want {
			t.Fatalf("sentenceCase(%q) = %q, want %q", input, got, want)
		}
	}
}
