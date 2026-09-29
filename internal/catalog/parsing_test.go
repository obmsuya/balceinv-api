package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
)

func TestParseRowsHandlesMessySpreadsheets(t *testing.T) {
	fileRows := [][]string{
		{"", ""},
		{" Product Name ", "Selling Price", "Category", "Strength", "SKU"},
		{"Paracetamol  500mg", "TSh 1,500", "Pain relief", "500mg", "para"},
		{},
		{"", "2000", "Pain relief"},
		{"paracetamol 500mg", "1800"},
		{"Amoxicillin", "abc"},
		{"ORS", "500/="},
		{"Zinc", "-3"},
	}

	parsedCatalog, parseError := ParseRows("pharmacy", fileRows, time.Now().UTC())
	if parseError != nil {
		t.Fatalf("unexpected parse error: %v", parseError)
	}
	if parsedCatalog.RowsRead != 6 || len(parsedCatalog.Products) != 2 {
		t.Fatalf("read %d rows into %d products, want 6 and 2: %+v", parsedCatalog.RowsRead, len(parsedCatalog.Products), parsedCatalog.Products)
	}

	paracetamol := parsedCatalog.Products[0]
	isParacetamolRight := paracetamol.Name == "Paracetamol 500mg" && paracetamol.DefaultPrice == 1500 &&
		paracetamol.SkuPrefix == "PARA" && paracetamol.Unit == "pcs" && paracetamol.BusinessType == "pharmacy" &&
		paracetamol.NameKey == "paracetamol 500mg" && paracetamol.Category != nil && *paracetamol.Category == "Pain relief"
	if !isParacetamolRight {
		t.Fatalf("paracetamol parsed wrong: %+v", paracetamol)
	}
	if string(paracetamol.Metadata) != `{"Strength":"500mg"}` {
		t.Fatalf("metadata = %s", paracetamol.Metadata)
	}
	if parsedCatalog.Products[1].Name != "ORS" || parsedCatalog.Products[1].DefaultPrice != 500 {
		t.Fatalf("ORS parsed wrong: %+v", parsedCatalog.Products[1])
	}

	wantProblemRows := []int{5, 6, 7, 9}
	if len(parsedCatalog.Problems) != len(wantProblemRows) {
		t.Fatalf("problems = %+v, want rows %v", parsedCatalog.Problems, wantProblemRows)
	}
	for problemIndex, wantRow := range wantProblemRows {
		if parsedCatalog.Problems[problemIndex].Row != wantRow {
			t.Fatalf("problem %d is on row %d, want %d", problemIndex, parsedCatalog.Problems[problemIndex].Row, wantRow)
		}
	}
	if parsedCatalog.Problems[1].Problem != "same name as row 3" {
		t.Fatalf("duplicate problem = %q", parsedCatalog.Problems[1].Problem)
	}
}

func TestParseRowsRejectsFilesWithoutNamesOrRows(t *testing.T) {
	now := time.Now().UTC()

	_, missingNameError := ParseRows("pharmacy", [][]string{{"price"}, {"100"}}, now)
	if !errors.Is(missingNameError, ErrNameColumnMissing) {
		t.Fatalf("missing name column error = %v", missingNameError)
	}
	_, headerOnlyError := ParseRows("pharmacy", [][]string{{"name"}, {" "}}, now)
	if !errors.Is(headerOnlyError, ErrFileEmpty) {
		t.Fatalf("header only error = %v", headerOnlyError)
	}
	_, blankFileError := ParseRows("pharmacy", nil, now)
	if !errors.Is(blankFileError, ErrFileEmpty) {
		t.Fatalf("blank file error = %v", blankFileError)
	}
}

func TestParsePrice(t *testing.T) {
	priceCases := map[string]int64{"1,500": 1500, "TZS 2 000": 2000, "": 0, "-": 0, "99.5": 100, "3000/=": 3000, "KES 250": 250}
	for rawPrice, wantPrice := range priceCases {
		parsedPrice, priceProblem := ParsePrice(rawPrice)
		if priceProblem != "" || parsedPrice != wantPrice {
			t.Fatalf("ParsePrice(%q) = %d, %q; want %d", rawPrice, parsedPrice, priceProblem, wantPrice)
		}
	}
	for _, badPrice := range []string{"free", "NaN", "1e20", "-5"} {
		_, priceProblem := ParsePrice(badPrice)
		if priceProblem == "" {
			t.Fatalf("ParsePrice(%q) was accepted", badPrice)
		}
	}
}

func TestSeedEmptyListsFillsOnlyEmptyValidLists(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigratedAsApp(t, engineCase)
		ctx := context.Background()
		catalogService := NewService(NewRepository())

		seedFiles := fstest.MapFS{
			"general.json":  {Data: []byte("")},
			"pharmacy.json": {Data: []byte(`[{"id":9,"name":"Panadol","business_type":"x","default_price":1200.4},{"name":" panadol "},{"name":""},{"name":"ORS","unit":"sachet","metadata":{"form":"Powder"}}]`)},
			"Bad-Type.json": {Data: []byte(`[{"name":"Ignored"}]`)},
		}

		firstSeedCount, firstSeedError := catalogService.seedEmptyListsFrom(ctx, openDatabase.Writer, seedFiles)
		if firstSeedError != nil || firstSeedCount != 2 {
			t.Fatalf("first seed = %d, %v; want 2", firstSeedCount, firstSeedError)
		}

		pharmacyList, listError := catalogService.Items(ctx, openDatabase.Writer, "pharmacy")
		if listError != nil || len(pharmacyList) != 2 {
			t.Fatalf("pharmacy list = %+v, %v", pharmacyList, listError)
		}
		isOrsRight := pharmacyList[0].Name == "ORS" && pharmacyList[0].Unit == "sachet" && pharmacyList[0].SkuPrefix == "GEN" &&
			strings.Contains(string(pharmacyList[0].Metadata), "Powder")
		isPanadolRight := pharmacyList[1].Name == "Panadol" && pharmacyList[1].Unit == "pcs" && pharmacyList[1].DefaultPrice == 1200 &&
			pharmacyList[1].BusinessType == "pharmacy"
		if !isOrsRight || !isPanadolRight {
			t.Fatalf("seeded entries not cleaned: %+v", pharmacyList)
		}

		secondSeedCount, secondSeedError := catalogService.seedEmptyListsFrom(ctx, openDatabase.Writer, seedFiles)
		if secondSeedError != nil || secondSeedCount != 0 {
			t.Fatalf("second seed = %d, %v; a filled list must not be seeded again", secondSeedCount, secondSeedError)
		}

		brokenFiles := fstest.MapFS{"hardware.json": {Data: []byte(`[{`)}}
		_, brokenSeedError := catalogService.seedEmptyListsFrom(ctx, openDatabase.Writer, brokenFiles)
		if brokenSeedError == nil {
			t.Fatal("a broken seed list should fail loudly")
		}

		shippedSeedCount, shippedSeedError := catalogService.SeedEmptyLists(ctx, openDatabase.Writer)
		if shippedSeedError != nil {
			t.Fatalf("shipped seed lists failed after seeding %d: %v", shippedSeedCount, shippedSeedError)
		}
	})
}
