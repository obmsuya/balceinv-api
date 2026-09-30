package businessmove

import (
	"testing"

	"github.com/google/uuid"
)

func TestOnlyTheBusinessOwnPicturesAreStored(t *testing.T) {
	companyId := uuid.MustParse("0190f7a2-0000-7000-8000-000000000001")
	otherCompanyId := uuid.MustParse("0190f7a2-0000-7000-8000-000000000002")
	cases := map[string]bool{
		"logos/" + companyId.String() + "/0190f7a2-0000-7000-8000-00000000000a.png":      true,
		"products/" + companyId.String() + "/0190f7a2-0000-7000-8000-00000000000b.jpg":   true,
		"logos/" + otherCompanyId.String() + "/0190f7a2-0000-7000-8000-00000000000a.png": false,
		"logos/" + companyId.String():                    false,
		"logos/../" + otherCompanyId.String() + "/x.png": false,
		"/logos/" + companyId.String() + "/x.png":        false,
		"logos/" + companyId.String() + "/sub/x.png":     true,
	}
	for mediaKey, wantAllowed := range cases {
		if isOwnMediaKey(mediaKey, companyId) != wantAllowed {
			t.Fatalf("isOwnMediaKey(%q) = %v, want %v", mediaKey, !wantAllowed, wantAllowed)
		}
	}
}
