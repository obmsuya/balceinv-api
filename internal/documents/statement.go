package documents

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

type StatementStyle int

const (
	StatementHeading StatementStyle = iota
	StatementItem
	StatementLine
	StatementSubtotal
	StatementTotal
	StatementGrandTotal
	StatementRatio
	StatementEmpty
)

type StatementTerm struct {
	Key      string
	Subtract bool
}

type StatementRow struct {
	Key              string
	Style            StatementStyle
	Label            string
	Code             string
	Current          int64
	Previous         int64
	IsDeduction      bool
	SumsSection      bool
	Terms            []StatementTerm
	RatioNumerator   string
	RatioDenominator string
}

type Statement struct {
	Language        string
	Title           string
	Subtitle        string
	CurrentHeading  string
	PreviousHeading string
	Filters         []Field
	GeneratedBy     string
	GeneratedAt     time.Time
	Rows            []StatementRow
	Notes           []string
	SheetName       string
}

var ErrStatementDoesNotAdd = errors.New("a statement total does not match its parts")

func RenderStatement(branding Branding, statement Statement, format string, baseName string) (File, error) {
	checkError := statement.Check()
	if checkError != nil {
		return File{}, checkError
	}
	switch format {
	case FormatExcel:
		workbookBytes, excelError := StatementExcel(branding, statement)
		if excelError != nil {
			return File{}, excelError
		}
		return File{Name: baseName + ".xlsx", ContentType: ExcelContentType, Bytes: workbookBytes}, nil
	case FormatPdf:
		pdfBytes, pdfError := StatementPdf(branding, statement)
		if pdfError != nil {
			return File{}, pdfError
		}
		return File{Name: baseName + ".pdf", ContentType: PdfContentType, Bytes: pdfBytes}, nil
	default:
		return File{}, ErrUnknownFormat
	}
}

func (statement Statement) Check() error {
	currentByKey := map[string]int64{}
	previousByKey := map[string]int64{}
	sectionCurrent, sectionPrevious := int64(0), int64(0)
	for _, statementRow := range statement.Rows {
		expectedCurrent, expectedPrevious, hasFormula := statementRow.Current, statementRow.Previous, false
		switch {
		case statementRow.SumsSection:
			expectedCurrent, expectedPrevious, hasFormula = sectionCurrent, sectionPrevious, true
		case len(statementRow.Terms) > 0:
			expectedCurrent, expectedPrevious, hasFormula = 0, 0, true
			for _, term := range statementRow.Terms {
				termSign := int64(1)
				if term.Subtract {
					termSign = -1
				}
				expectedCurrent += termSign * currentByKey[term.Key]
				expectedPrevious += termSign * previousByKey[term.Key]
			}
		}
		if hasFormula && (expectedCurrent != statementRow.Current || expectedPrevious != statementRow.Previous) {
			return fmt.Errorf("%w: %s", ErrStatementDoesNotAdd, statementRow.Label)
		}
		if statementRow.Style == StatementHeading {
			sectionCurrent, sectionPrevious = 0, 0
		}
		if statementRow.Style == StatementItem {
			sectionCurrent += statementRow.Current
			sectionPrevious += statementRow.Previous
		}
		if statementRow.Key != "" {
			currentByKey[statementRow.Key] = statementRow.Current
			previousByKey[statementRow.Key] = statementRow.Previous
		}
	}
	return nil
}

func (statement Statement) language() string {
	return ResolveLanguage(statement.Language)
}

func (statement Statement) valueOf(key string, usePrevious bool) int64 {
	for _, statementRow := range statement.Rows {
		if statementRow.Key == key {
			if usePrevious {
				return statementRow.Previous
			}
			return statementRow.Current
		}
	}
	return 0
}

func (statement Statement) ratio(statementRow StatementRow, usePrevious bool) (int64, bool) {
	denominator := statement.valueOf(statementRow.RatioDenominator, usePrevious)
	if denominator == 0 {
		return 0, false
	}
	numerator := statement.valueOf(statementRow.RatioNumerator, usePrevious)
	return numerator * 10000 / denominator, true
}

func StatementAmount(minorUnits int64, decimals int) string {
	if minorUnits == 0 {
		return "–"
	}
	if minorUnits < 0 {
		return "(" + FormatMoney(-minorUnits, decimals) + ")"
	}
	return FormatMoney(minorUnits, decimals)
}

func (statementRow StatementRow) Change() string {
	if statementRow.IsDeduction {
		return StatementChange(absolute(statementRow.Current), absolute(statementRow.Previous))
	}
	return StatementChange(statementRow.Current, statementRow.Previous)
}

func StatementChange(current int64, previous int64) string {
	if previous == 0 {
		return "–"
	}
	changeBasisPoints := (current - previous) * 10000 / absolute(previous)
	changeText := strconv.FormatFloat(float64(changeBasisPoints)/100, 'f', 1, 64) + "%"
	if changeBasisPoints > 0 {
		return "+" + changeText
	}
	return changeText
}

func absolute(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
