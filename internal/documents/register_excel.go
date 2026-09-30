package documents

import (
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type registerExcelWriter struct {
	*documentSheet
	register Register
}

func RegisterExcel(branding Branding, register Register) ([]byte, error) {
	heading := sheetHeading{
		title:       register.Title,
		subtitle:    register.Subtitle,
		filters:     register.Filters,
		generatedBy: register.GeneratedBy,
		generatedAt: register.GeneratedAt,
	}
	sheet, sheetError := newDocumentSheet(branding, register.Language, heading, register.SheetName)
	if sheetError != nil {
		return nil, sheetError
	}
	writer := &registerExcelWriter{documentSheet: sheet, register: register}

	columnWidths := []float64{}
	isFirstText := true
	for _, column := range register.Columns {
		columnWidths = append(columnWidths, registerExcelWidth(column.Kind, isFirstText))
		if column.Kind == Text {
			isFirstText = false
		}
	}
	widthError := writer.setColumnWidths(columnWidths)
	if widthError != nil {
		sheet.workbook.Close()
		return nil, widthError
	}

	headerRow, writeError := writer.writeRegisterSheet(heading)
	if writeError != nil {
		sheet.workbook.Close()
		return nil, writeError
	}
	setupError := writer.writeSetup(headerRow, register.Landscape)
	if setupError != nil {
		sheet.workbook.Close()
		return nil, setupError
	}
	return sheet.finish()
}

func registerExcelWidth(kind Kind, isFirstText bool) float64 {
	switch kind {
	case Text:
		if isFirstText {
			return 40
		}
		return 24
	case Date:
		return 13
	case DateTime:
		return 17
	case Integer, Percent:
		return 10
	default:
		return 17
	}
}

func (writer *registerExcelWriter) lastColumn() int {
	return max(len(writer.register.Columns), 2)
}

func (writer *registerExcelWriter) writeRegisterSheet(heading sheetHeading) (int, error) {
	currentRow, letterheadError := writer.writeLetterhead(heading, writer.lastColumn())
	if letterheadError != nil {
		return 0, letterheadError
	}

	if len(writer.register.Summary) > 0 {
		for _, summaryField := range writer.register.Summary {
			summaryError := writer.writeSummaryLine(currentRow, summaryField)
			if summaryError != nil {
				return 0, summaryError
			}
			currentRow++
		}
		currentRow++
	}

	headerRow := currentRow
	for columnIndex, column := range writer.register.Columns {
		headerStyle := sheetCellStyle{bold: true, headerEnd: true, size: 9, alignEnd: isNumericKind(column.Kind), wrap: true}
		headerError := writer.writeCell(headerRow, columnIndex+1, column.Title, headerStyle)
		if headerError != nil {
			return 0, headerError
		}
	}
	heightError := writer.workbook.SetRowHeight(writer.sheetName, headerRow, 22)
	if heightError != nil {
		return 0, fmt.Errorf("failed to size the header: %w", heightError)
	}
	currentRow++

	balanceIndex := writer.register.balanceColumn()
	previousBalanceCell := ""
	if writer.register.OpeningBalance != nil && balanceIndex >= 0 {
		openingError := writer.writeLabelledBalance(currentRow, writer.register.OpeningLabel, *writer.register.OpeningBalance, "", sheetCellStyle{italic: true}, sheetCellStyle{italic: true, alignEnd: true, format: writer.moneyFormat()})
		if openingError != nil {
			return 0, openingError
		}
		previousBalanceCell = cellAt(balanceIndex+1, currentRow)
		currentRow++
	}

	firstLineRow := currentRow
	for _, registerRow := range writer.register.Rows {
		lineError := writer.writeLine(currentRow, registerRow, previousBalanceCell)
		if lineError != nil {
			return 0, lineError
		}
		if balanceIndex >= 0 {
			previousBalanceCell = cellAt(balanceIndex+1, currentRow)
		}
		currentRow++
	}
	lastLineRow := currentRow - 1

	if len(writer.register.Rows) == 0 && writer.register.EmptyText != "" {
		emptyError := writer.writeMerged(currentRow, writer.register.EmptyText, sheetCellStyle{italic: true, muted: true}, writer.lastColumn())
		if emptyError != nil {
			return 0, emptyError
		}
		currentRow++
	}

	if writer.register.hasSums() && len(writer.register.Rows) > 0 {
		totalsError := writer.writeTotals(currentRow, firstLineRow, lastLineRow)
		if totalsError != nil {
			return 0, totalsError
		}
		currentRow++
	}

	if balanceIndex >= 0 {
		closingFormula := ""
		if previousBalanceCell != "" {
			closingFormula = previousBalanceCell
		}
		closingError := writer.writeLabelledBalance(currentRow, writer.register.ClosingLabel, writer.register.closingBalance(), closingFormula,
			sheetCellStyle{bold: true, fill: true},
			sheetCellStyle{bold: true, fill: true, topRule: true, doubleEnd: true, alignEnd: true, format: writer.moneyFormat()})
		if closingError != nil {
			return 0, closingError
		}
		currentRow++
	}

	if len(writer.register.Rows) > 0 {
		filterError := writer.workbook.AutoFilter(writer.sheetName, cellAt(1, headerRow)+":"+cellAt(len(writer.register.Columns), lastLineRow), nil)
		if filterError != nil {
			return 0, fmt.Errorf("failed to add filters to the header: %w", filterError)
		}
	}

	notesEndRow, notesError := writer.writeNotes(currentRow+1, writer.register.Notes, writer.lastColumn())
	if notesError != nil {
		return 0, notesError
	}
	return headerRow, writer.writeSignatures(notesEndRow+1, writer.register.GeneratedBy, writer.lastColumn())
}

func (writer *registerExcelWriter) writeSummaryLine(sheetRow int, summaryField Field) error {
	lastColumn := writer.lastColumn()
	if lastColumn > 2 {
		mergeError := writer.workbook.MergeCell(writer.sheetName, cellAt(1, sheetRow), cellAt(lastColumn-1, sheetRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge a summary label: %w", mergeError)
		}
	}
	labelError := writer.writeCell(sheetRow, 1, summaryField.Label, sheetCellStyle{muted: true})
	if labelError != nil {
		return labelError
	}
	valueStyle := sheetCellStyle{alignEnd: true, bold: summaryField.Strong}
	var summaryValue any = FormatValue(writer.branding, writer.language, summaryField.Kind, summaryField.Value)
	if summaryField.Kind == Money {
		valueStyle.format = writer.moneyFormat()
		summaryValue = writer.majorUnits(moneyOf(summaryField.Value))
	}
	return writer.writeCell(sheetRow, lastColumn, summaryValue, valueStyle)
}

func (writer *registerExcelWriter) writeLine(sheetRow int, registerRow RegisterRow, previousBalanceCell string) error {
	for columnIndex, column := range writer.register.Columns {
		cellValue := cellOf(registerRow, columnIndex)
		cellStyle := sheetCellStyle{rowRule: true, alignEnd: isNumericKind(column.Kind)}
		if column.Balance {
			balanceAmount, _ := asInteger(cellValue)
			balanceError := writer.writeFormula(sheetRow, columnIndex+1, writer.balanceFormula(sheetRow, previousBalanceCell), writer.majorUnits(balanceAmount), sheetCellStyle{rowRule: true, alignEnd: true, format: writer.moneyFormat()})
			if balanceError != nil {
				return balanceError
			}
			continue
		}
		excelValue, valueFormat := writer.excelValue(column.Kind, cellValue)
		cellStyle.format = valueFormat
		cellError := writer.writeCell(sheetRow, columnIndex+1, excelValue, cellStyle)
		if cellError != nil {
			return cellError
		}
	}
	return nil
}

func (writer *registerExcelWriter) balanceFormula(sheetRow int, previousBalanceCell string) string {
	formulaParts := []string{}
	if previousBalanceCell != "" {
		formulaParts = append(formulaParts, previousBalanceCell)
	}
	for columnIndex, column := range writer.register.Columns {
		switch column.Effect {
		case AddsToBalance:
			formulaParts = append(formulaParts, "+N("+cellAt(columnIndex+1, sheetRow)+")")
		case TakesFromBalance:
			formulaParts = append(formulaParts, "-N("+cellAt(columnIndex+1, sheetRow)+")")
		}
	}
	formula := strings.TrimPrefix(strings.Join(formulaParts, ""), "+")
	if formula == "" {
		return "0"
	}
	return formula
}

func (writer *registerExcelWriter) writeTotals(sheetRow int, firstLineRow int, lastLineRow int) error {
	firstSumColumn := 1
	for firstSumColumn <= len(writer.register.Columns) && !writer.register.Columns[firstSumColumn-1].Sum {
		firstSumColumn++
	}
	if firstSumColumn > 2 {
		mergeError := writer.workbook.MergeCell(writer.sheetName, cellAt(1, sheetRow), cellAt(firstSumColumn-1, sheetRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge the total label: %w", mergeError)
		}
	}
	if firstSumColumn > 1 {
		labelError := writer.writeCell(sheetRow, 1, writer.register.TotalLabel, sheetCellStyle{bold: true})
		if labelError != nil {
			return labelError
		}
	}
	for columnIndex := firstSumColumn - 1; columnIndex < len(writer.register.Columns); columnIndex++ {
		if !writer.register.Columns[columnIndex].Sum {
			continue
		}
		columnName, nameError := excelize.ColumnNumberToName(columnIndex + 1)
		if nameError != nil {
			return fmt.Errorf("failed to name a total column: %w", nameError)
		}
		sumFormula := fmt.Sprintf("SUM(%s%d:%s%d)", columnName, firstLineRow, columnName, lastLineRow)
		totalValue, totalFormat := writer.excelValue(writer.register.Columns[columnIndex].Kind, writer.register.columnTotal(columnIndex))
		totalError := writer.writeFormula(sheetRow, columnIndex+1, sumFormula, totalValue, sheetCellStyle{bold: true, topRule: true, alignEnd: true, format: totalFormat})
		if totalError != nil {
			return totalError
		}
	}
	return nil
}

func (writer *registerExcelWriter) writeLabelledBalance(sheetRow int, label string, balance int64, formula string, labelStyle sheetCellStyle, balanceStyle sheetCellStyle) error {
	balanceColumn := writer.register.balanceColumn() + 1
	if balanceColumn > 2 {
		mergeError := writer.workbook.MergeCell(writer.sheetName, cellAt(1, sheetRow), cellAt(balanceColumn-1, sheetRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge a balance label: %w", mergeError)
		}
	}
	labelError := writer.writeCell(sheetRow, 1, label, labelStyle)
	if labelError != nil {
		return labelError
	}
	if formula != "" {
		return writer.writeFormula(sheetRow, balanceColumn, formula, writer.majorUnits(balance), balanceStyle)
	}
	return writer.writeCell(sheetRow, balanceColumn, writer.majorUnits(balance), balanceStyle)
}

func (writer *registerExcelWriter) excelValue(kind Kind, cellValue any) (any, string) {
	switch typedValue := cellValue.(type) {
	case nil:
		return nil, ""
	case time.Time:
		localMoment := typedValue.In(writer.branding.Location())
		if kind == DateTime {
			return time.Date(localMoment.Year(), localMoment.Month(), localMoment.Day(), localMoment.Hour(), localMoment.Minute(), 0, 0, time.UTC), "dd/mm/yyyy hh:mm"
		}
		return time.Date(localMoment.Year(), localMoment.Month(), localMoment.Day(), 0, 0, 0, 0, time.UTC), "dd/mm/yyyy"
	case string:
		return typedValue, ""
	}
	integerValue, isInteger := asInteger(cellValue)
	if !isInteger {
		return fmt.Sprint(cellValue), ""
	}
	switch kind {
	case Money:
		return writer.majorUnits(integerValue), writer.moneyFormat()
	case Percent:
		return float64(integerValue) / 10000, "0.0%"
	default:
		return integerValue, "#,##0"
	}
}

func isNumericKind(kind Kind) bool {
	return kind == Money || kind == Integer || kind == Percent
}
