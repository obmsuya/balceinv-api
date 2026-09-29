package stock

import "testing"

func TestCrossedThresholdFiresOnlyOnTheWayDown(t *testing.T) {
	cases := []struct {
		before  int
		after   int
		minimum int
		want    string
	}{
		{10, 5, 5, "low_stock"},
		{6, 1, 5, "low_stock"},
		{5, 4, 5, ""},
		{4, 0, 5, "out_of_stock"},
		{10, 0, 5, "out_of_stock"},
		{0, 3, 5, ""},
		{3, 8, 5, ""},
		{2, 0, 0, "out_of_stock"},
		{3, 1, 0, ""},
	}
	for _, crossingCase := range cases {
		got := crossedThreshold(crossingCase.before, crossingCase.after, crossingCase.minimum)
		if got != crossingCase.want {
			t.Fatalf("crossedThreshold(%d, %d, %d) = %q, want %q", crossingCase.before, crossingCase.after, crossingCase.minimum, got, crossingCase.want)
		}
	}
}
