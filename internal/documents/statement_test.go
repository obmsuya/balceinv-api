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

func sampleStatement(expenseCount int) documents.Statement {
	statementRows := []documents.StatementRow{
		{Style: documents.StatementHeading, Label: "Revenue"},
		{Style: documents.StatementItem, Code: "4000", Label: "Sales", Current: 1000000, Previous: 800000},
		{Style: documents.StatementItem, Code: "4100", Label: "Other income", Current: 50000},
		{Key: "revenue", Style: documents.StatementSubtotal, Label: "Total revenue", Current: 1050000, Previous: 800000, SumsSection: true},
		{Key: "cogs", Style: documents.StatementLine, Label: "Less: cost of goods sold", Current: -600000, Previous: -500000, IsDeduction: true},
		{Key: "gross", Style: documents.StatementTotal, Label: "Gross profit", Current: 450000, Previous: 300000, Terms: []documents.StatementTerm{{Key: "revenue"}, {Key: "cogs"}}},
		{Style: documents.StatementRatio, Label: "Gross margin", RatioNumerator: "gross", RatioDenominator: "revenue"},
		{Style: documents.StatementHeading, Label: "Operating expenses"},
	}
	expenseTotal := int64(0)
	for expenseIndex := range expenseCount {
		expenseAmount := int64(10000 + expenseIndex*100)
		expenseTotal += expenseAmount
		statementRows = append(statementRows, documents.StatementRow{Style: documents.StatementItem, Code: fmt.Sprintf("6%03d", expenseIndex), Label: fmt.Sprintf("Expense %d", expenseIndex), Current: expenseAmount})
	}
	if expenseCount == 0 {
		statementRows = append(statementRows, documents.StatementRow{Style: documents.StatementEmpty, Label: "No expenses recorded in this period"})
	}
	statementRows = append(statementRows,
		documents.StatementRow{Key: "expenses", Style: documents.StatementSubtotal, Label: "Total operating expenses", Current: expenseTotal, SumsSection: true},
		documents.StatementRow{Key: "net", Style: documents.StatementGrandTotal, Label: "Net profit", Current: 450000 - expenseTotal, Previous: 300000, Terms: []documents.StatementTerm{{Key: "gross"}, {Key: "expenses", Subtract: true}}},
		documents.StatementRow{Style: documents.StatementRatio, Label: "Net margin", RatioNumerator: "net", RatioDenominator: "revenue"},
	)
	return documents.Statement{
		Language:        documents.English,
		Title:           "Profit and loss",
		Subtitle:        "1 Sep 2026 – 30 Sep 2026",
		CurrentHeading:  "Sep 2026",
		PreviousHeading: "Aug 2026",
		Filters:         []documents.Field{{Label: "Shop", Value: "All shops"}},
		GeneratedBy:     "Amina Juma",
		GeneratedAt:     time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC),
		Rows:            statementRows,
		Notes:           []string{"Amounts in brackets reduce profit."},
		SheetName:       "Profit and loss",
	}
}

func statementBranding() documents.Branding {
	return documents.Branding{CompanyName: "Duka la Amina", Tin: "123-456-789", CurrencyCode: "TZS", Timezone: "Africa/Dar_es_Salaam", PrimaryColor: "#5EA500"}
}

func TestStatementRefusesTotalsThatDoNotAdd(t *testing.T) {
	statement := sampleStatement(3)
	if checkError := statement.Check(); checkError != nil {
		t.Fatalf("a correct statement was refused: %v", checkError)
	}
	statement.Rows[len(statement.Rows)-2].Current++
	_, renderError := documents.RenderStatement(statementBranding(), statement, documents.FormatPdf, "broken")
	if !errors.Is(renderError, documents.ErrStatementDoesNotAdd) {
		t.Fatalf("a net profit that does not add up was rendered: %v", renderError)
	}
}

func TestStatementAmountsReadLikeAnAccountant(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{documents.StatementAmount(0, 0), "–"},
		{documents.StatementAmount(1234567, 0), "1,234,567"},
		{documents.StatementAmount(-1200, 0), "(1,200)"},
		{documents.StatementAmount(-123456, 2), "(1,234.56)"},
		{documents.StatementChange(110, 100), "+10.0%"},
		{documents.StatementChange(50, 100), "-50.0%"},
		{documents.StatementChange(-50, -100), "+50.0%"},
		{documents.StatementChange(100, 0), "–"},
		{documents.StatementRow{Current: -600, Previous: -500, IsDeduction: true}.Change(), "+20.0%"},
	}
	for _, testCase := range cases {
		if testCase.got != testCase.want {
			t.Errorf("got %q, want %q", testCase.got, testCase.want)
		}
	}
}

func TestStatementWorkbookUsesFormulasThatAgreeWithTheStatement(t *testing.T) {
	for _, expenseCount := range []int{0, 4} {
		statement := sampleStatement(expenseCount)
		exported, renderError := documents.RenderStatement(statementBranding(), statement, documents.FormatExcel, "profit-and-loss")
		if renderError != nil {
			t.Fatalf("render: %v", renderError)
		}
		workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Bytes))
		if openError != nil {
			t.Fatalf("open: %v", openError)
		}
		sheetName := workbook.GetSheetName(0)
		if sheetName != "Profit and loss" {
			t.Fatalf("sheet is %q", sheetName)
		}

		rowOfLabel := map[string]int{}
		sheetRows, rowsError := workbook.GetRows(sheetName)
		if rowsError != nil {
			t.Fatalf("rows: %v", rowsError)
		}
		for rowIndex, sheetRow := range sheetRows {
			if len(sheetRow) > 1 {
				rowOfLabel[sheetRow[1]] = rowIndex + 1
			}
		}

		for _, statementRow := range statement.Rows {
			if statementRow.Key == "" {
				continue
			}
			sheetRow := rowOfLabel[statementRow.Label]
			for columnName, want := range map[string]int64{"C": statementRow.Current, "D": statementRow.Previous} {
				cellName := columnName + strconv.Itoa(sheetRow)
				formula, _ := workbook.GetCellFormula(sheetName, cellName)
				isCalculated := statementRow.SumsSection || len(statementRow.Terms) > 0
				if isCalculated && formula == "" {
					t.Errorf("%s (%s) has no formula", statementRow.Label, cellName)
				}
				calculated, calcError := workbook.CalcCellValue(sheetName, cellName, excelize.Options{RawCellValue: true})
				if calcError != nil {
					t.Fatalf("calculating %s: %v", cellName, calcError)
				}
				if calculated != strconv.FormatInt(want, 10) {
					t.Errorf("%d expenses: %s %s calculates to %s, want %d", expenseCount, statementRow.Label, cellName, calculated, want)
				}
			}
		}

		panes, panesError := workbook.GetPanes(sheetName)
		if panesError != nil || !panes.Freeze {
			t.Errorf("the column headings are not frozen: %v", panesError)
		}
		definedNames := workbook.GetDefinedName()
		hasPrintTitles := false
		for _, definedName := range definedNames {
			hasPrintTitles = hasPrintTitles || definedName.Name == "_xlnm.Print_Titles"
		}
		if !hasPrintTitles {
			t.Error("the column headings do not repeat when printed")
		}
		workbook.Close()
	}
}

func TestLongStatementsBreakAcrossPagesWithoutLosingRows(t *testing.T) {
	pagePattern := regexp.MustCompile(`/Type /Page\b[^s]`)
	shortStatement, shortError := documents.RenderStatement(statementBranding(), sampleStatement(3), documents.FormatPdf, "short")
	if shortError != nil {
		t.Fatalf("short: %v", shortError)
	}
	longStatement, longError := documents.RenderStatement(statementBranding(), sampleStatement(120), documents.FormatPdf, "long")
	if longError != nil {
		t.Fatalf("long: %v", longError)
	}
	if !bytes.HasPrefix(shortStatement.Bytes, []byte("%PDF")) || shortStatement.ContentType != documents.PdfContentType {
		t.Fatal("the statement is not a PDF")
	}
	shortPages := len(pagePattern.FindAll(shortStatement.Bytes, -1))
	longPages := len(pagePattern.FindAll(longStatement.Bytes, -1))
	if shortPages != 1 || longPages < 3 {
		t.Fatalf("pages: short %d, long %d", shortPages, longPages)
	}
}
