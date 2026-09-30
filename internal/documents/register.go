package documents

import (
	"errors"
	"fmt"
	"time"
)

type BalanceEffect int

const (
	NoEffect BalanceEffect = iota
	AddsToBalance
	TakesFromBalance
)

type RegisterColumn struct {
	Title   string
	Kind    Kind
	Sum     bool
	Weight  float64
	Effect  BalanceEffect
	Balance bool
}

type RegisterRow struct {
	Cells []any
}

type Register struct {
	Language       string
	Title          string
	Subtitle       string
	Filters        []Field
	Summary        []Field
	GeneratedBy    string
	GeneratedAt    time.Time
	Landscape      bool
	Columns        []RegisterColumn
	Rows           []RegisterRow
	OpeningLabel   string
	OpeningBalance *int64
	TotalLabel     string
	ClosingLabel   string
	EmptyText      string
	Notes          []string
	SheetName      string
}

var ErrRegisterDoesNotAdd = errors.New("a register balance does not follow from its lines")

func RenderRegister(branding Branding, register Register, format string, baseName string) (File, error) {
	checkError := register.Check()
	if checkError != nil {
		return File{}, checkError
	}
	switch format {
	case FormatExcel:
		workbookBytes, excelError := RegisterExcel(branding, register)
		if excelError != nil {
			return File{}, excelError
		}
		return File{Name: baseName + ".xlsx", ContentType: ExcelContentType, Bytes: workbookBytes}, nil
	case FormatPdf:
		pdfBytes, pdfError := RegisterPdf(branding, register)
		if pdfError != nil {
			return File{}, pdfError
		}
		return File{Name: baseName + ".pdf", ContentType: PdfContentType, Bytes: pdfBytes}, nil
	default:
		return File{}, ErrUnknownFormat
	}
}

func (register Register) balanceColumn() int {
	for columnIndex, column := range register.Columns {
		if column.Balance {
			return columnIndex
		}
	}
	return -1
}

func (register Register) hasSums() bool {
	for _, column := range register.Columns {
		if column.Sum {
			return true
		}
	}
	return false
}

func (register Register) Check() error {
	balanceIndex := register.balanceColumn()
	if balanceIndex < 0 {
		return nil
	}
	runningBalance := int64(0)
	if register.OpeningBalance != nil {
		runningBalance = *register.OpeningBalance
	}
	for rowIndex, registerRow := range register.Rows {
		for columnIndex, column := range register.Columns {
			cellAmount, _ := asInteger(cellOf(registerRow, columnIndex))
			switch column.Effect {
			case AddsToBalance:
				runningBalance += cellAmount
			case TakesFromBalance:
				runningBalance -= cellAmount
			}
		}
		statedBalance, _ := asInteger(cellOf(registerRow, balanceIndex))
		if statedBalance != runningBalance {
			return fmt.Errorf("%w: line %d says %d, the lines add up to %d", ErrRegisterDoesNotAdd, rowIndex+1, statedBalance, runningBalance)
		}
	}
	return nil
}

func (register Register) closingBalance() int64 {
	balanceIndex := register.balanceColumn()
	if len(register.Rows) > 0 && balanceIndex >= 0 {
		lastBalance, _ := asInteger(cellOf(register.Rows[len(register.Rows)-1], balanceIndex))
		return lastBalance
	}
	if register.OpeningBalance != nil {
		return *register.OpeningBalance
	}
	return 0
}

func (register Register) columnTotal(columnIndex int) int64 {
	columnTotal := int64(0)
	for _, registerRow := range register.Rows {
		cellAmount, _ := asInteger(cellOf(registerRow, columnIndex))
		columnTotal += cellAmount
	}
	return columnTotal
}

func (register Register) language() string {
	return ResolveLanguage(register.Language)
}

func cellOf(registerRow RegisterRow, columnIndex int) any {
	if columnIndex < len(registerRow.Cells) {
		return registerRow.Cells[columnIndex]
	}
	return nil
}
