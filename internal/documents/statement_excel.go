package documents

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	statementExcelCodeColumn     = 1
	statementExcelLabelColumn    = 2
	statementExcelCurrentColumn  = 3
	statementExcelPreviousColumn = 4
	statementExcelChangeColumn   = 5
)

type statementExcelWriter struct {
	*documentSheet
	statement Statement
	rowOfKey  map[string]int
}

type statementHeaderCell struct {
	column int
	text   string
	style  sheetCellStyle
}

type statementRatioColumn struct {
	column      int
	usePrevious bool
}

func StatementExcel(branding Branding, statement Statement) ([]byte, error) {
	sheet, sheetError := newDocumentSheet(branding, statement.Language, statement.heading(), statement.SheetName)
	if sheetError != nil {
		return nil, sheetError
	}
	writer := &statementExcelWriter{documentSheet: sheet, statement: statement, rowOfKey: map[string]int{}}

	columnWidths := []float64{9, 46, 18, 18, 11}
	if statement.SingleColumn {
		columnWidths = []float64{9, 52, 20}
	}
	widthError := writer.setColumnWidths(columnWidths)
	if widthError != nil {
		sheet.workbook.Close()
		return nil, widthError
	}
	headerRow, sheetWriteError := writer.writeStatementSheet()
	if sheetWriteError != nil {
		sheet.workbook.Close()
		return nil, sheetWriteError
	}
	setupError := writer.writeSetup(headerRow, false)
	if setupError != nil {
		sheet.workbook.Close()
		return nil, setupError
	}
	return sheet.finish()
}

func (statement Statement) heading() sheetHeading {
	return sheetHeading{
		title:       statement.Title,
		subtitle:    statement.Subtitle,
		filters:     statement.Filters,
		generatedBy: statement.GeneratedBy,
		generatedAt: statement.GeneratedAt,
	}
}

func (writer *statementExcelWriter) lastColumn() int {
	if writer.statement.SingleColumn {
		return statementExcelCurrentColumn
	}
	return statementExcelChangeColumn
}

func (writer *statementExcelWriter) writeStatementSheet() (int, error) {
	currentRow, letterheadError := writer.writeLetterhead(writer.statement.heading(), writer.lastColumn())
	if letterheadError != nil {
		return 0, letterheadError
	}

	headerRow := currentRow
	headerError := writer.writeHeader(headerRow)
	if headerError != nil {
		return 0, headerError
	}
	currentRow++

	sectionStartRow := currentRow
	for _, statementRow := range writer.statement.Rows {
		if statementRow.Style == StatementHeading {
			sectionStartRow = currentRow + 1
		}
		rowError := writer.writeStatementRow(currentRow, sectionStartRow, statementRow)
		if rowError != nil {
			return 0, rowError
		}
		if statementRow.Key != "" {
			writer.rowOfKey[statementRow.Key] = currentRow
		}
		currentRow++
	}

	notesEndRow, notesError := writer.writeNotes(currentRow+1, writer.statement.Notes, writer.lastColumn())
	if notesError != nil {
		return 0, notesError
	}
	return headerRow, writer.writeSignatures(notesEndRow+1, writer.statement.GeneratedBy, writer.lastColumn())
}

func (writer *statementExcelWriter) writeHeader(headerRow int) error {
	headerCells := []statementHeaderCell{
		{statementExcelCodeColumn, Label(writer.language, "statementCode"), sheetCellStyle{bold: true, headerEnd: true, size: 9}},
		{statementExcelLabelColumn, writer.currencyHeading(), sheetCellStyle{muted: true, headerEnd: true, size: 9}},
		{statementExcelCurrentColumn, writer.statement.CurrentHeading, sheetCellStyle{bold: true, headerEnd: true, alignEnd: true, size: 9}},
	}
	if !writer.statement.SingleColumn {
		headerCells = append(headerCells,
			statementHeaderCell{statementExcelPreviousColumn, writer.statement.PreviousHeading, sheetCellStyle{bold: true, muted: true, headerEnd: true, alignEnd: true, size: 9}},
			statementHeaderCell{statementExcelChangeColumn, Label(writer.language, "statementChange"), sheetCellStyle{bold: true, muted: true, headerEnd: true, alignEnd: true, size: 9}},
		)
	}
	for _, headerCell := range headerCells {
		writeError := writer.writeCell(headerRow, headerCell.column, headerCell.text, headerCell.style)
		if writeError != nil {
			return writeError
		}
	}
	return writer.workbook.SetRowHeight(writer.sheetName, headerRow, 20)
}

func (writer *statementExcelWriter) currencyHeading() string {
	if writer.branding.CurrencyCode == "" || writer.statement.SingleColumn {
		return ""
	}
	return Label(writer.language, "statementAmountsIn", "{currency}", writer.branding.CurrencyCode)
}

func (writer *statementExcelWriter) writeStatementRow(sheetRow int, sectionStartRow int, statementRow StatementRow) error {
	switch statementRow.Style {
	case StatementHeading:
		return writer.writeMerged(sheetRow, strings.ToUpper(statementRow.Label), sheetCellStyle{bold: true, brand: true, size: 10}, statementExcelLabelColumn)
	case StatementEmpty:
		return writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, sheetCellStyle{italic: true, muted: true, indent: 1})
	case StatementRatio:
		return writer.writeRatio(sheetRow, statementRow)
	}

	labelStyle := sheetCellStyle{}
	amountStyle := sheetCellStyle{alignEnd: true, format: writer.moneyFormat()}
	switch statementRow.Style {
	case StatementItem:
		labelStyle.indent = 1
	case StatementSubtotal:
		labelStyle.bold, amountStyle.bold, amountStyle.topRule = true, true, true
	case StatementTotal:
		labelStyle.bold, labelStyle.fill = true, true
		amountStyle.bold, amountStyle.topRule, amountStyle.fill = true, true, true
	case StatementGrandTotal:
		labelStyle.bold, labelStyle.fill, labelStyle.size = true, true, 11
		amountStyle.bold, amountStyle.topRule, amountStyle.doubleEnd, amountStyle.fill, amountStyle.size = true, true, true, true, 11
	}
	codeStyle := sheetCellStyle{muted: true, fill: labelStyle.fill}

	codeError := writer.writeCell(sheetRow, statementExcelCodeColumn, statementRow.Code, codeStyle)
	if codeError != nil {
		return codeError
	}
	labelError := writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, labelStyle)
	if labelError != nil {
		return labelError
	}
	currentError := writer.writeAmount(sheetRow, statementExcelCurrentColumn, sectionStartRow, statementRow, statementRow.Current, amountStyle)
	if currentError != nil || writer.statement.SingleColumn {
		return currentError
	}

	previousStyle := amountStyle
	previousStyle.muted = true
	previousError := writer.writeAmount(sheetRow, statementExcelPreviousColumn, sectionStartRow, statementRow, statementRow.Previous, previousStyle)
	if previousError != nil {
		return previousError
	}

	changeStyle := sheetCellStyle{alignEnd: true, muted: true, format: "+0.0%;-0.0%;0.0%", topRule: amountStyle.topRule, doubleEnd: amountStyle.doubleEnd, fill: amountStyle.fill}
	currentCell := cellAt(statementExcelCurrentColumn, sheetRow)
	previousCell := cellAt(statementExcelPreviousColumn, sheetRow)
	changeFormula := fmt.Sprintf(`IF(%s=0,"–",(%s-%s)/ABS(%s))`, previousCell, currentCell, previousCell, previousCell)
	changeValue := statementChangeRatio(statementRow.Current, statementRow.Previous)
	if statementRow.IsDeduction {
		changeFormula = fmt.Sprintf(`IF(%s=0,"–",(ABS(%s)-ABS(%s))/ABS(%s))`, previousCell, currentCell, previousCell, previousCell)
		changeValue = statementChangeRatio(absolute(statementRow.Current), absolute(statementRow.Previous))
	}
	return writer.writeFormula(sheetRow, statementExcelChangeColumn, changeFormula, changeValue, changeStyle)
}

func (writer *statementExcelWriter) writeAmount(sheetRow int, column int, sectionStartRow int, statementRow StatementRow, minorUnits int64, style sheetCellStyle) error {
	columnName, nameError := excelize.ColumnNumberToName(column)
	if nameError != nil {
		return fmt.Errorf("failed to name an amount column: %w", nameError)
	}
	cachedValue := writer.majorUnits(minorUnits)
	switch {
	case statementRow.SumsSection:
		sumFormula := fmt.Sprintf("SUM(%s%d:%s%d)", columnName, sectionStartRow, columnName, max(sheetRow-1, sectionStartRow))
		return writer.writeFormula(sheetRow, column, sumFormula, cachedValue, style)
	case len(statementRow.Terms) > 0:
		formulaParts := []string{}
		for termIndex, term := range statementRow.Terms {
			operator := "+"
			if term.Subtract {
				operator = "-"
			}
			if termIndex == 0 && !term.Subtract {
				operator = ""
			}
			formulaParts = append(formulaParts, fmt.Sprintf("%s%s%d", operator, columnName, writer.rowOfKey[term.Key]))
		}
		return writer.writeFormula(sheetRow, column, strings.Join(formulaParts, ""), cachedValue, style)
	default:
		return writer.writeCell(sheetRow, column, cachedValue, style)
	}
}

func (writer *statementExcelWriter) writeRatio(sheetRow int, statementRow StatementRow) error {
	labelError := writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, sheetCellStyle{italic: true, muted: true, indent: 1})
	if labelError != nil {
		return labelError
	}
	ratioStyle := sheetCellStyle{italic: true, muted: true, alignEnd: true, format: "0.0%"}
	ratioColumns := []statementRatioColumn{{statementExcelCurrentColumn, false}}
	if !writer.statement.SingleColumn {
		ratioColumns = append(ratioColumns, statementRatioColumn{statementExcelPreviousColumn, true})
	}
	for _, ratioColumn := range ratioColumns {
		columnName, nameError := excelize.ColumnNumberToName(ratioColumn.column)
		if nameError != nil {
			return fmt.Errorf("failed to name a ratio column: %w", nameError)
		}
		numeratorCell := fmt.Sprintf("%s%d", columnName, writer.rowOfKey[statementRow.RatioNumerator])
		denominatorCell := fmt.Sprintf("%s%d", columnName, writer.rowOfKey[statementRow.RatioDenominator])
		ratioFormula := fmt.Sprintf(`IF(%s=0,"–",%s/%s)`, denominatorCell, numeratorCell, denominatorCell)
		var cachedValue any = "–"
		ratioBasisPoints, hasRatio := writer.statement.ratio(statementRow, ratioColumn.usePrevious)
		if hasRatio {
			cachedValue = float64(ratioBasisPoints) / 10000
		}
		formulaError := writer.writeFormula(sheetRow, ratioColumn.column, ratioFormula, cachedValue, ratioStyle)
		if formulaError != nil {
			return formulaError
		}
	}
	return nil
}

func statementChangeRatio(current int64, previous int64) any {
	if previous == 0 {
		return "–"
	}
	return float64(current-previous) / float64(absolute(previous))
}
