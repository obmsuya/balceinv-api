package services

import (
	"errors"
	"testing"
	"testing/fstest"
)

func TestParseCatalogRowsHandlesMessySpreadsheets(t *testing.T) {
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

	parsedCatalog, parseError := ParseCatalogRows("pharmacy", fileRows)
	if parseError != nil {
		t.Fatalf("unexpected parse error: %v", parseError)
	}

	if parsedCatalog.RowsRead != 6 {
		t.Fatalf("rows read = %d, want 6", parsedCatalog.RowsRead)
	}
	if len(parsedCatalog.Products) != 2 {
		t.Fatalf("products = %d, want 2: %+v", len(parsedCatalog.Products), parsedCatalog.Products)
	}

	paracetamol := parsedCatalog.Products[0]
	if paracetamol.Name != "Paracetamol 500mg" || paracetamol.DefaultPrice != 1500 || paracetamol.SKUPrefix != "PARA" {
		t.Fatalf("paracetamol parsed wrong: %+v", paracetamol)
	}
	if paracetamol.Category == nil || *paracetamol.Category != "Pain relief" {
		t.Fatalf("category parsed wrong: %+v", paracetamol.Category)
	}
	if paracetamol.Metadata["Strength"] != "500mg" || paracetamol.Unit != "pcs" || paracetamol.BusinessType != "pharmacy" {
		t.Fatalf("metadata or defaults wrong: %+v", paracetamol)
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

func TestParseCatalogRowsRejectsFilesWithoutNamesOrRows(t *testing.T) {
	_, missingNameError := ParseCatalogRows("pharmacy", [][]string{{"price"}, {"100"}})
	if !errors.Is(missingNameError, ErrCatalogNameColumnMissing) {
		t.Fatalf("missing name column error = %v", missingNameError)
	}
	_, headerOnlyError := ParseCatalogRows("pharmacy", [][]string{{"name"}, {" "}})
	if !errors.Is(headerOnlyError, ErrCatalogFileEmpty) {
		t.Fatalf("header only error = %v", headerOnlyError)
	}
	_, blankFileError := ParseCatalogRows("pharmacy", nil)
	if !errors.Is(blankFileError, ErrCatalogFileEmpty) {
		t.Fatalf("blank file error = %v", blankFileError)
	}
}

func TestParseCatalogCSVHandlesExcelExports(t *testing.T) {
	csvRows, csvError := parseCatalogCSV([]byte("\xef\xbb\xbfname;price\r\nPanadol;1 200\r\n"))
	if csvError != nil {
		t.Fatalf("unexpected csv error: %v", csvError)
	}
	if len(csvRows) != 2 || csvRows[0][0] != "name" || csvRows[1][1] != "1 200" {
		t.Fatalf("csv rows = %q", csvRows)
	}
}

func TestReadSeedCatalogSkipsEmptyAndCleansEntries(t *testing.T) {
	seedFiles := fstest.MapFS{
		"general.json":  {Data: []byte("")},
		"pharmacy.json": {Data: []byte(`[{"id":9,"name":"Panadol","business_type":"x"},{"name":" panadol "},{"name":""},{"name":"ORS","unit":"sachet"}]`)},
		"broken.json":   {Data: []byte(`[{`)},
	}

	emptySeed, emptySeedError := readSeedCatalog(seedFiles, "general")
	if emptySeedError != nil || emptySeed != nil {
		t.Fatalf("empty seed = %+v, %v", emptySeed, emptySeedError)
	}
	missingSeed, missingSeedError := readSeedCatalog(seedFiles, "hardware")
	if missingSeedError != nil || missingSeed != nil {
		t.Fatalf("missing seed = %+v, %v", missingSeed, missingSeedError)
	}
	if _, brokenSeedError := readSeedCatalog(seedFiles, "broken"); brokenSeedError == nil {
		t.Fatal("broken seed should fail")
	}

	pharmacySeed, pharmacySeedError := readSeedCatalog(seedFiles, "pharmacy")
	if pharmacySeedError != nil {
		t.Fatalf("pharmacy seed error: %v", pharmacySeedError)
	}
	if len(pharmacySeed) != 2 {
		t.Fatalf("pharmacy seed = %+v, want 2 entries", pharmacySeed)
	}
	if pharmacySeed[0].ID != 0 || pharmacySeed[0].BusinessType != "pharmacy" || pharmacySeed[0].Unit != "pcs" {
		t.Fatalf("first seed entry not cleaned: %+v", pharmacySeed[0])
	}
	if pharmacySeed[1].Unit != "sachet" || pharmacySeed[1].SKUPrefix != "GEN" {
		t.Fatalf("second seed entry wrong: %+v", pharmacySeed[1])
	}
}

func TestParseCatalogPrice(t *testing.T) {
	priceCases := map[string]float64{"1,500": 1500, "TZS 2 000": 2000, "": 0, "-": 0, "99.5": 99.5, "3000/=": 3000}
	for rawPrice, wantPrice := range priceCases {
		parsedPrice, priceProblem := ParseCatalogPrice(rawPrice)
		if priceProblem != "" || parsedPrice != wantPrice {
			t.Fatalf("ParseCatalogPrice(%q) = %v, %q; want %v", rawPrice, parsedPrice, priceProblem, wantPrice)
		}
	}
	if _, priceProblem := ParseCatalogPrice("free"); priceProblem == "" {
		t.Fatal("a word should not be accepted as a price")
	}
}
