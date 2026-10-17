package products_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestImportRowLimitCountsOnlyFilledRows(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Blank Rows", "owner@blankrows.test")

		trailingBlanks := strings.Builder{}
		trailingBlanks.WriteString("name,sku,price,quantity\nRice,RICE,3000,2\nBeans,BEANS,2500,1\n")
		for blankRow := 0; blankRow < 20001; blankRow++ {
			trailingBlanks.WriteString(",,,\n")
		}
		withBlanks := importCsv(harness, company.OwnerToken, "formatted.csv", trailingBlanks.String())
		if withBlanks.Status != http.StatusCreated || withBlanks.Data()["created"] != float64(2) {
			t.Fatalf("a short sheet with trailing blank rows returned %d %v", withBlanks.Status, withBlanks.Body)
		}

		tooMany := strings.Builder{}
		tooMany.WriteString("name,sku,price\n")
		for rowNumber := 0; rowNumber < 20001; rowNumber++ {
			fmt.Fprintf(&tooMany, "Item %d,LIMIT-%05d,100\n", rowNumber, rowNumber)
		}
		overLimit := importCsv(harness, company.OwnerToken, "huge.csv", tooMany.String())
		if overLimit.Code() != "invalid_import" {
			t.Fatalf("a sheet over the row limit returned %d %v", overLimit.Status, overLimit.Body)
		}
	})
}
