package documents_test

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/xuri/excelize/v2"
)

func sampleRegister(lineCount int) documents.Register {
	openingBalance := int64(40000)
	runningBalance := openingBalance
	registerRows := []documents.RegisterRow{}
	for lineIndex := range lineCount {
		moneyIn, moneyOut := int64(0), int64(0)
		if lineIndex%3 == 2 {
			moneyOut = int64(7000 + lineIndex)
		} else {
			moneyIn = int64(2500 + lineIndex*10)
		}
		runningBalance += moneyIn - moneyOut
		var inCell, outCell any
		if moneyIn != 0 {
			inCell = moneyIn
		}
		if moneyOut != 0 {
			outCell = moneyOut
		}
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{time.Date(2026, 9, 1+lineIndex%28, 10, 0, 0, 0, time.UTC), fmt.Sprintf("JE-%06d", lineIndex+1), "Sale", inCell, outCell, runningBalance}})
	}
	return documents.Register{
		Language:    documents.English,
		Title:       "Account statement",
		Subtitle:    "1000 · Cash · 1 Sep 2026 – 30 Sep 2026",
		GeneratedBy: "Amina Juma",
		GeneratedAt: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Summary:     []documents.Field{{Label: "Opening balance", Value: openingBalance, Kind: documents.Money}},
		Columns: []documents.RegisterColumn{
			{Title: "Date", Kind: documents.Date},
			{Title: "Entry", Kind: documents.Text},
			{Title: "Description", Kind: documents.Text},
			{Title: "Money in (TZS)", Kind: documents.Money, Sum: true, Effect: documents.AddsToBalance},
			{Title: "Money out (TZS)", Kind: documents.Money, Sum: true, Effect: documents.TakesFromBalance},
			{Title: "Balance (TZS)", Kind: documents.Money, Balance: true},
		},
		Rows:           registerRows,
		OpeningLabel:   "Opening balance",
		OpeningBalance: &openingBalance,
		TotalLabel:     "Total for the period",
		ClosingLabel:   "Closing balance",
		EmptyText:      "No entries in this period",
		Notes:          []string{"Every line is an entry in the ledger."},
		SheetName:      "Account statement",
	}
}

func TestRegisterRefusesABalanceThatDoesNotFollowFromItsLines(t *testing.T) {
	register := sampleRegister(5)
	if checkError := register.Check(); checkError != nil {
		t.Fatalf("a correct register was refused: %v", checkError)
	}
	register.Rows[3].Cells[5] = int64(1)
	_, renderError := documents.RenderRegister(statementBranding(), register, documents.FormatExcel, "broken")
	if !errors.Is(renderError, documents.ErrRegisterDoesNotAdd) {
		t.Fatalf("a wrong running balance was rendered: %v", renderError)
	}
}

func TestRegisterWorkbookRunsItsBalanceWithFormulas(t *testing.T) {
	for _, lineCount := range []int{0, 1, 12} {
		register := sampleRegister(lineCount)
		exported, renderError := documents.RenderRegister(statementBranding(), register, documents.FormatExcel, "statement")
		if renderError != nil {
			t.Fatalf("render: %v", renderError)
		}
		workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Bytes))
		if openError != nil {
			t.Fatalf("open: %v", openError)
		}
		sheetName := workbook.GetSheetName(0)
		sheetRows, _ := workbook.GetRows(sheetName)
		rowOf := map[string]int{}
		for rowIndex, sheetRow := range sheetRows {
			if len(sheetRow) > 0 {
				rowOf[sheetRow[0]] = rowIndex + 1
			}
		}

		closingRow := rowOf["Closing balance"]
		closingFormula, _ := workbook.GetCellFormula(sheetName, "F"+strconv.Itoa(closingRow))
		closing, calcError := workbook.CalcCellValue(sheetName, "F"+strconv.Itoa(closingRow), excelize.Options{RawCellValue: true})
		wantClosing := int64(40000)
		if lineCount > 0 {
			wantClosing, _ = register.Rows[lineCount-1].Cells[5].(int64)
		}
		if calcError != nil || closing != strconv.FormatInt(wantClosing, 10) || (lineCount > 0 && closingFormula == "") {
			t.Fatalf("%d lines: closing calculates to %q (%v, formula %q), want %d", lineCount, closing, calcError, closingFormula, wantClosing)
		}
		if lineCount > 0 {
			totalRow := rowOf["Total for the period"]
			for columnName, columnIndex := range map[string]int{"D": 3, "E": 4} {
				want := int64(0)
				for _, registerRow := range register.Rows {
					amount, _ := registerRow.Cells[columnIndex].(int64)
					want += amount
				}
				total, _ := workbook.CalcCellValue(sheetName, columnName+strconv.Itoa(totalRow), excelize.Options{RawCellValue: true})
				if total != strconv.FormatInt(want, 10) {
					t.Errorf("%d lines: column %s totals %s, want %d", lineCount, columnName, total, want)
				}
			}
		} else if rowOf["No entries in this period"] == 0 {
			t.Error("an empty register does not say so")
		}
		workbook.Close()
	}
}

func TestLongRegistersRepeatTheirHeaderOnEveryPage(t *testing.T) {
	pagePattern := regexp.MustCompile(`/Type /Page\b[^s]`)
	longRegister, renderError := documents.RenderRegister(statementBranding(), sampleRegister(150), documents.FormatPdf, "long")
	if renderError != nil {
		t.Fatalf("render: %v", renderError)
	}
	if pageCount := len(pagePattern.FindAll(longRegister.Bytes, -1)); pageCount < 3 {
		t.Fatalf("150 lines fit on %d pages", pageCount)
	}
	emptyRegister, emptyError := documents.RenderRegister(statementBranding(), sampleRegister(0), documents.FormatPdf, "empty")
	if emptyError != nil || !bytes.HasPrefix(emptyRegister.Bytes, []byte("%PDF")) {
		t.Fatalf("an empty register did not render: %v", emptyError)
	}
}
