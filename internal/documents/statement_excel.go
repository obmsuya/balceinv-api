package documents

import (
	"fmt"
	"math"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	statementExcelCodeColumn     = 1
	statementExcelLabelColumn    = 2
	statementExcelCurrentColumn  = 3
	statementExcelPreviousColumn = 4
	statementExcelChangeColumn   = 5
	statementExcelLastColumn     = statementExcelChangeColumn
	statementExcelNoteCharacters = 110
)

type statementExcelWriter struct {
	*excelWriter
	statement  Statement
	sheetName  string
	cellStyles map[string]int
	rowOfKey   map[string]int
	headerRow  int
}

type statementCellStyle struct {
	bold      bool
	italic    bool
	muted     bool
	brand     bool
	size      float64
	indent    int
	fill      bool
	topRule   bool
	doubleEnd bool
	alignEnd  bool
	wrap      bool
	format    string
	headerEnd bool
}

func StatementExcel(branding Branding, statement Statement) ([]byte, error) {
	workbook := excelize.NewFile()
	defer workbook.Close()

	baseWriter := &excelWriter{
		workbook: workbook,
		branding: branding,
		document: Document{
			Language:    statement.Language,
			Title:       statement.Title,
			Subtitle:    statement.Subtitle,
			GeneratedBy: statement.GeneratedBy,
			GeneratedAt: statement.GeneratedAt,
		},
		language:   statement.language(),
		styles:     map[excelStyleKey]int{},
		sheetNames: map[string]bool{},
	}
	writer := &statementExcelWriter{
		excelWriter: baseWriter,
		statement:   statement,
		sheetName:   baseWriter.uniqueSheetName(firstNonEmpty(statement.SheetName, statement.Title)),
		cellStyles:  map[string]int{},
		rowOfKey:    map[string]int{},
	}
	renameError := workbook.SetSheetName(workbook.GetSheetName(0), writer.sheetName)
	if renameError != nil {
		return nil, fmt.Errorf("failed to name the statement sheet: %w", renameError)
	}

	for _, step := range []func() error{writer.writeColumns, writer.writeSheet, writer.writeSetup} {
		stepError := step()
		if stepError != nil {
			return nil, stepError
		}
	}
	propertiesError := writer.writeProperties()
	if propertiesError != nil {
		return nil, propertiesError
	}
	fullCalculation := true
	calcError := workbook.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalculation})
	if calcError != nil {
		return nil, fmt.Errorf("failed to ask Excel to recalculate: %w", calcError)
	}

	workbookBuffer, writeError := workbook.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("failed to write the statement workbook: %w", writeError)
	}
	return workbookBuffer.Bytes(), nil
}

func (writer *statementExcelWriter) writeColumns() error {
	columnWidths := map[int]float64{
		statementExcelCodeColumn:     9,
		statementExcelLabelColumn:    46,
		statementExcelCurrentColumn:  18,
		statementExcelPreviousColumn: 18,
		statementExcelChangeColumn:   11,
	}
	for columnNumber, columnWidth := range columnWidths {
		columnName, nameError := excelize.ColumnNumberToName(columnNumber)
		if nameError != nil {
			return fmt.Errorf("failed to name a statement column: %w", nameError)
		}
		widthError := writer.workbook.SetColWidth(writer.sheetName, columnName, columnName, columnWidth)
		if widthError != nil {
			return fmt.Errorf("failed to size a statement column: %w", widthError)
		}
	}
	return nil
}

func (writer *statementExcelWriter) writeSheet() error {
	currentRow := 1
	letterheadLines := []struct {
		text  string
		style statementCellStyle
	}{
		{writer.branding.CompanyName, statementCellStyle{bold: true, brand: true, size: 14}},
		{writer.branding.taxLine(writer.language), statementCellStyle{muted: true, size: 9}},
		{writer.branding.contactLine(), statementCellStyle{muted: true, size: 9}},
	}
	for _, letterheadLine := range letterheadLines {
		if letterheadLine.text == "" {
			continue
		}
		writeError := writer.writeMerged(currentRow, letterheadLine.text, letterheadLine.style, statementExcelPreviousColumn)
		if writeError != nil {
			return writeError
		}
		currentRow++
	}
	logoError := writer.addLogo(writer.sheetName, statementExcelChangeColumn)
	if logoError != nil {
		return logoError
	}
	currentRow++

	filterParts := []string{}
	for _, filter := range writer.statement.Filters {
		filterParts = append(filterParts, filter.Label+": "+FormatValue(writer.branding, writer.language, filter.Kind, filter.Value))
	}
	titleLines := []struct {
		text  string
		style statementCellStyle
	}{
		{writer.statement.Title, statementCellStyle{bold: true, size: 16}},
		{writer.statement.Subtitle, statementCellStyle{size: 11}},
		{strings.Join(filterParts, "  ·  "), statementCellStyle{muted: true, size: 9}},
		{writer.generatedLine(), statementCellStyle{muted: true, italic: true, size: 9}},
	}
	for _, titleLine := range titleLines {
		if titleLine.text == "" {
			continue
		}
		writeError := writer.writeMerged(currentRow, titleLine.text, titleLine.style, statementExcelLastColumn)
		if writeError != nil {
			return writeError
		}
		currentRow++
	}
	currentRow++

	writer.headerRow = currentRow
	headerError := writer.writeHeader(writer.headerRow)
	if headerError != nil {
		return headerError
	}
	currentRow++

	sectionStartRow := currentRow
	for _, statementRow := range writer.statement.Rows {
		if statementRow.Style == StatementHeading {
			sectionStartRow = currentRow + 1
		}
		rowError := writer.writeStatementRow(currentRow, sectionStartRow, statementRow)
		if rowError != nil {
			return rowError
		}
		if statementRow.Key != "" {
			writer.rowOfKey[statementRow.Key] = currentRow
		}
		currentRow++
	}

	currentRow++
	notesEndRow, notesError := writer.writeNotes(currentRow)
	if notesError != nil {
		return notesError
	}
	return writer.writeSignatures(notesEndRow + 1)
}

func (writer *statementExcelWriter) writeHeader(headerRow int) error {
	headerCells := []struct {
		column int
		text   string
		style  statementCellStyle
	}{
		{statementExcelCodeColumn, Label(writer.language, "statementCode"), statementCellStyle{bold: true, headerEnd: true, size: 9}},
		{statementExcelLabelColumn, writer.currencyHeading(), statementCellStyle{muted: true, headerEnd: true, size: 9}},
		{statementExcelCurrentColumn, writer.statement.CurrentHeading, statementCellStyle{bold: true, headerEnd: true, alignEnd: true, size: 9}},
		{statementExcelPreviousColumn, writer.statement.PreviousHeading, statementCellStyle{bold: true, muted: true, headerEnd: true, alignEnd: true, size: 9}},
		{statementExcelChangeColumn, Label(writer.language, "statementChange"), statementCellStyle{bold: true, muted: true, headerEnd: true, alignEnd: true, size: 9}},
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
	if writer.branding.CurrencyCode == "" {
		return ""
	}
	return Label(writer.language, "statementAmountsIn", "{currency}", writer.branding.CurrencyCode)
}

func (writer *statementExcelWriter) writeStatementRow(sheetRow int, sectionStartRow int, statementRow StatementRow) error {
	switch statementRow.Style {
	case StatementHeading:
		return writer.writeMerged(sheetRow, strings.ToUpper(statementRow.Label), statementCellStyle{bold: true, brand: true, size: 10}, statementExcelLabelColumn)
	case StatementEmpty:
		return writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, statementCellStyle{italic: true, muted: true, indent: 1})
	case StatementRatio:
		return writer.writeRatio(sheetRow, statementRow)
	}

	labelStyle := statementCellStyle{}
	amountStyle := statementCellStyle{alignEnd: true, format: writer.moneyFormat()}
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
	previousStyle := amountStyle
	previousStyle.muted = true
	changeStyle := statementCellStyle{alignEnd: true, muted: true, format: "+0.0%;-0.0%;0.0%", topRule: amountStyle.topRule, doubleEnd: amountStyle.doubleEnd, fill: amountStyle.fill}
	codeStyle := statementCellStyle{muted: true, fill: labelStyle.fill}

	codeError := writer.writeCell(sheetRow, statementExcelCodeColumn, statementRow.Code, codeStyle)
	if codeError != nil {
		return codeError
	}
	labelError := writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, labelStyle)
	if labelError != nil {
		return labelError
	}

	amountColumns := []struct {
		column int
		value  int64
		style  statementCellStyle
	}{
		{statementExcelCurrentColumn, statementRow.Current, amountStyle},
		{statementExcelPreviousColumn, statementRow.Previous, previousStyle},
	}
	for _, amountColumn := range amountColumns {
		amountError := writer.writeAmount(sheetRow, amountColumn.column, sectionStartRow, statementRow, amountColumn.value, amountColumn.style)
		if amountError != nil {
			return amountError
		}
	}

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

func (writer *statementExcelWriter) writeAmount(sheetRow int, column int, sectionStartRow int, statementRow StatementRow, minorUnits int64, style statementCellStyle) error {
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
	labelError := writer.writeCell(sheetRow, statementExcelLabelColumn, statementRow.Label, statementCellStyle{italic: true, muted: true, indent: 1})
	if labelError != nil {
		return labelError
	}
	ratioStyle := statementCellStyle{italic: true, muted: true, alignEnd: true, format: "0.0%"}
	for _, ratioColumn := range []struct {
		column      int
		usePrevious bool
	}{{statementExcelCurrentColumn, false}, {statementExcelPreviousColumn, true}} {
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

func (writer *statementExcelWriter) writeNotes(startRow int) (int, error) {
	if len(writer.statement.Notes) == 0 {
		return startRow, nil
	}
	headingError := writer.writeCell(startRow, statementExcelCodeColumn, Label(writer.language, "statementNotes"), statementCellStyle{bold: true})
	if headingError != nil {
		return 0, headingError
	}
	noteRow := startRow + 1
	for noteIndex, note := range writer.statement.Notes {
		numberError := writer.writeCell(noteRow, statementExcelCodeColumn, fmt.Sprintf("%d.", noteIndex+1), statementCellStyle{muted: true, alignEnd: true})
		if numberError != nil {
			return 0, numberError
		}
		noteStartCell := cellAt(statementExcelLabelColumn, noteRow)
		mergeError := writer.workbook.MergeCell(writer.sheetName, noteStartCell, cellAt(statementExcelLastColumn, noteRow))
		if mergeError != nil {
			return 0, fmt.Errorf("failed to merge a note: %w", mergeError)
		}
		noteError := writer.writeCell(noteRow, statementExcelLabelColumn, note, statementCellStyle{muted: true, wrap: true})
		if noteError != nil {
			return 0, noteError
		}
		lineCount := math.Ceil(float64(len([]rune(note))) / statementExcelNoteCharacters)
		heightError := writer.workbook.SetRowHeight(writer.sheetName, noteRow, math.Max(lineCount, 1)*14)
		if heightError != nil {
			return 0, fmt.Errorf("failed to size a note: %w", heightError)
		}
		noteRow++
	}
	return noteRow, nil
}

func (writer *statementExcelWriter) writeSignatures(startRow int) error {
	signatureRow := startRow + 1
	signatureLines := []struct {
		label string
		value string
	}{
		{Label(writer.language, "statementPreparedBy"), nameOrBalce(writer.statement.GeneratedBy)},
		{Label(writer.language, "statementCheckedBy"), ""},
		{Label(writer.language, "statementDate"), ""},
	}
	for _, signatureLine := range signatureLines {
		labelError := writer.writeCell(signatureRow, statementExcelLabelColumn, signatureLine.label, statementCellStyle{muted: true})
		if labelError != nil {
			return labelError
		}
		mergeError := writer.workbook.MergeCell(writer.sheetName, cellAt(statementExcelCurrentColumn, signatureRow), cellAt(statementExcelLastColumn, signatureRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge a signature line: %w", mergeError)
		}
		valueError := writer.writeCell(signatureRow, statementExcelCurrentColumn, signatureLine.value, statementCellStyle{headerEnd: true})
		for column := statementExcelCurrentColumn + 1; column <= statementExcelLastColumn && valueError == nil; column++ {
			valueError = writer.writeCell(signatureRow, column, nil, statementCellStyle{headerEnd: true})
		}
		if valueError != nil {
			return valueError
		}
		heightError := writer.workbook.SetRowHeight(writer.sheetName, signatureRow, 26)
		if heightError != nil {
			return fmt.Errorf("failed to size a signature line: %w", heightError)
		}
		signatureRow++
	}
	return nil
}

func (writer *statementExcelWriter) writeSetup() error {
	gridLinesVisible := false
	viewError := writer.workbook.SetSheetView(writer.sheetName, 0, &excelize.ViewOptions{ShowGridLines: &gridLinesVisible})
	if viewError != nil {
		return fmt.Errorf("failed to hide the grid lines: %w", viewError)
	}
	return writer.writePrintSetup(writer.sheetName, writer.headerRow, true, false)
}

func (writer *statementExcelWriter) writeMerged(sheetRow int, value string, style statementCellStyle, lastColumn int) error {
	if lastColumn > statementExcelCodeColumn {
		mergeError := writer.workbook.MergeCell(writer.sheetName, cellAt(statementExcelCodeColumn, sheetRow), cellAt(lastColumn, sheetRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge a heading: %w", mergeError)
		}
	}
	return writer.writeCell(sheetRow, statementExcelCodeColumn, value, style)
}

func (writer *statementExcelWriter) writeCell(sheetRow int, column int, value any, style statementCellStyle) error {
	cellName := cellAt(column, sheetRow)
	if value != nil {
		valueError := writer.workbook.SetCellValue(writer.sheetName, cellName, value)
		if valueError != nil {
			return fmt.Errorf("failed to write cell %s: %w", cellName, valueError)
		}
	}
	return writer.applyStyle(cellName, style)
}

func (writer *statementExcelWriter) writeFormula(sheetRow int, column int, formula string, cachedValue any, style statementCellStyle) error {
	cellName := cellAt(column, sheetRow)
	valueError := writer.workbook.SetCellValue(writer.sheetName, cellName, cachedValue)
	if valueError != nil {
		return fmt.Errorf("failed to write cell %s: %w", cellName, valueError)
	}
	formulaError := writer.workbook.SetCellFormula(writer.sheetName, cellName, formula)
	if formulaError != nil {
		return fmt.Errorf("failed to write the formula in %s: %w", cellName, formulaError)
	}
	return writer.applyStyle(cellName, style)
}

func (writer *statementExcelWriter) applyStyle(cellName string, style statementCellStyle) error {
	styleKey := fmt.Sprintf("%+v", style)
	styleId, isKnown := writer.cellStyles[styleKey]
	if !isKnown {
		newStyleId, styleError := writer.workbook.NewStyle(writer.excelStyle(style))
		if styleError != nil {
			return fmt.Errorf("failed to create a statement style: %w", styleError)
		}
		writer.cellStyles[styleKey] = newStyleId
		styleId = newStyleId
	}
	return writer.workbook.SetCellStyle(writer.sheetName, cellName, cellName, styleId)
}

func (writer *statementExcelWriter) excelStyle(style statementCellStyle) *excelize.Style {
	fontSize := style.size
	if fontSize == 0 {
		fontSize = 10
	}
	fontColor := "1C2128"
	if style.muted {
		fontColor = excelMutedColor
	}
	if style.brand {
		fontColor = strings.TrimPrefix(writer.branding.PrimaryColor, "#")
		if fontColor == "" {
			fontColor = strings.TrimPrefix(defaultBrandColor, "#")
		}
	}
	horizontal := "left"
	if style.alignEnd {
		horizontal = "right"
	}
	excelStyle := &excelize.Style{
		Font:      &excelize.Font{Bold: style.bold, Italic: style.italic, Size: fontSize, Color: fontColor, Family: "Calibri"},
		Alignment: &excelize.Alignment{Horizontal: horizontal, Vertical: "center", Indent: style.indent, WrapText: style.wrap},
	}
	if style.fill {
		excelStyle.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F1F4F7"}}
	}
	borders := []excelize.Border{}
	if style.topRule {
		borders = append(borders, excelize.Border{Type: "top", Color: "3C4450", Style: 1})
	}
	if style.doubleEnd {
		borders = append(borders, excelize.Border{Type: "bottom", Color: "3C4450", Style: 6})
	}
	if style.headerEnd {
		borders = append(borders, excelize.Border{Type: "bottom", Color: strings.TrimPrefix(firstNonEmpty(writer.branding.PrimaryColor, defaultBrandColor), "#"), Style: 2})
	}
	excelStyle.Border = borders
	if style.format != "" {
		numberFormat := style.format
		excelStyle.CustomNumFmt = &numberFormat
	}
	return excelStyle
}

func (writer *statementExcelWriter) moneyFormat() string {
	if writer.branding.CurrencyDecimals > 0 {
		decimalPart := "." + strings.Repeat("0", writer.branding.CurrencyDecimals)
		return `#,##0` + decimalPart + `;(#,##0` + decimalPart + `);"–"`
	}
	return `#,##0;(#,##0);"–"`
}

func (writer *statementExcelWriter) majorUnits(minorUnits int64) any {
	if writer.branding.CurrencyDecimals <= 0 {
		return minorUnits
	}
	return float64(minorUnits) / math.Pow10(writer.branding.CurrencyDecimals)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func statementChangeRatio(current int64, previous int64) any {
	if previous == 0 {
		return "–"
	}
	return float64(current-previous) / float64(absolute(previous))
}
