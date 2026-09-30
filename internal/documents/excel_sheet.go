package documents

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const excelNoteCharactersPerLine = 110

type sheetCellStyle struct {
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
	rowRule   bool
}

type documentSheet struct {
	*excelWriter
	sheetName  string
	cellStyles map[string]int
}

type sheetHeading struct {
	title       string
	subtitle    string
	filters     []Field
	generatedBy string
	generatedAt time.Time
}

func newDocumentSheet(branding Branding, language string, heading sheetHeading, sheetName string) (*documentSheet, error) {
	workbook := excelize.NewFile()
	baseWriter := &excelWriter{
		workbook: workbook,
		branding: branding,
		document: Document{
			Language:    language,
			Title:       heading.title,
			Subtitle:    heading.subtitle,
			GeneratedBy: heading.generatedBy,
			GeneratedAt: heading.generatedAt,
		},
		language:   ResolveLanguage(language),
		styles:     map[excelStyleKey]int{},
		sheetNames: map[string]bool{},
	}
	sheet := &documentSheet{
		excelWriter: baseWriter,
		sheetName:   baseWriter.uniqueSheetName(firstNonEmpty(sheetName, heading.title)),
		cellStyles:  map[string]int{},
	}
	renameError := workbook.SetSheetName(workbook.GetSheetName(0), sheet.sheetName)
	if renameError != nil {
		workbook.Close()
		return nil, fmt.Errorf("failed to name the sheet: %w", renameError)
	}
	return sheet, nil
}

func (sheet *documentSheet) finish() ([]byte, error) {
	defer sheet.workbook.Close()
	propertiesError := sheet.writeProperties()
	if propertiesError != nil {
		return nil, propertiesError
	}
	fullCalculation := true
	calcError := sheet.workbook.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalculation})
	if calcError != nil {
		return nil, fmt.Errorf("failed to ask Excel to recalculate: %w", calcError)
	}
	workbookBuffer, writeError := sheet.workbook.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("failed to write the workbook: %w", writeError)
	}
	return workbookBuffer.Bytes(), nil
}

func (sheet *documentSheet) setColumnWidths(columnWidths []float64) error {
	for columnIndex, columnWidth := range columnWidths {
		columnName, nameError := excelize.ColumnNumberToName(columnIndex + 1)
		if nameError != nil {
			return fmt.Errorf("failed to name a column: %w", nameError)
		}
		widthError := sheet.workbook.SetColWidth(sheet.sheetName, columnName, columnName, columnWidth)
		if widthError != nil {
			return fmt.Errorf("failed to size a column: %w", widthError)
		}
	}
	return nil
}

func (sheet *documentSheet) writeLetterhead(heading sheetHeading, lastColumn int) (int, error) {
	currentRow := 1
	letterheadLines := []struct {
		text  string
		style sheetCellStyle
	}{
		{sheet.branding.CompanyName, sheetCellStyle{bold: true, brand: true, size: 14}},
		{sheet.branding.taxLine(sheet.language), sheetCellStyle{muted: true, size: 9}},
		{sheet.branding.contactLine(), sheetCellStyle{muted: true, size: 9}},
	}
	for _, letterheadLine := range letterheadLines {
		if letterheadLine.text == "" {
			continue
		}
		writeError := sheet.writeMerged(currentRow, letterheadLine.text, letterheadLine.style, max(lastColumn-1, 1))
		if writeError != nil {
			return 0, writeError
		}
		currentRow++
	}
	logoError := sheet.addLogo(sheet.sheetName, lastColumn)
	if logoError != nil {
		return 0, logoError
	}
	currentRow++

	filterParts := []string{}
	for _, filter := range heading.filters {
		filterParts = append(filterParts, filter.Label+": "+FormatValue(sheet.branding, sheet.language, filter.Kind, filter.Value))
	}
	titleLines := []struct {
		text  string
		style sheetCellStyle
	}{
		{heading.title, sheetCellStyle{bold: true, size: 16}},
		{heading.subtitle, sheetCellStyle{size: 11}},
		{strings.Join(filterParts, "  ·  "), sheetCellStyle{muted: true, size: 9}},
		{sheet.generatedLine(), sheetCellStyle{muted: true, italic: true, size: 9}},
	}
	for _, titleLine := range titleLines {
		if titleLine.text == "" {
			continue
		}
		writeError := sheet.writeMerged(currentRow, titleLine.text, titleLine.style, lastColumn)
		if writeError != nil {
			return 0, writeError
		}
		currentRow++
	}
	return currentRow + 1, nil
}

func (sheet *documentSheet) writeNotes(startRow int, notes []string, lastColumn int) (int, error) {
	if len(notes) == 0 {
		return startRow, nil
	}
	headingError := sheet.writeCell(startRow, 1, Label(sheet.language, "statementNotes"), sheetCellStyle{bold: true})
	if headingError != nil {
		return 0, headingError
	}
	noteRow := startRow + 1
	for noteIndex, note := range notes {
		numberError := sheet.writeCell(noteRow, 1, fmt.Sprintf("%d.", noteIndex+1), sheetCellStyle{muted: true, alignEnd: true})
		if numberError != nil {
			return 0, numberError
		}
		mergeError := sheet.workbook.MergeCell(sheet.sheetName, cellAt(2, noteRow), cellAt(lastColumn, noteRow))
		if mergeError != nil {
			return 0, fmt.Errorf("failed to merge a note: %w", mergeError)
		}
		noteError := sheet.writeCell(noteRow, 2, note, sheetCellStyle{muted: true, wrap: true})
		if noteError != nil {
			return 0, noteError
		}
		lineCount := math.Ceil(float64(len([]rune(note))) / excelNoteCharactersPerLine)
		heightError := sheet.workbook.SetRowHeight(sheet.sheetName, noteRow, math.Max(lineCount, 1)*14)
		if heightError != nil {
			return 0, fmt.Errorf("failed to size a note: %w", heightError)
		}
		noteRow++
	}
	return noteRow, nil
}

func (sheet *documentSheet) writeSignatures(startRow int, preparedBy string, lastColumn int) error {
	lineFirstColumn := max(lastColumn-2, 2)
	signatureRow := startRow + 1
	signatureLines := []struct {
		label string
		value string
	}{
		{Label(sheet.language, "statementPreparedBy"), nameOrBalce(preparedBy)},
		{Label(sheet.language, "statementCheckedBy"), ""},
		{Label(sheet.language, "statementDate"), ""},
	}
	for _, signatureLine := range signatureLines {
		if lineFirstColumn > 2 {
			labelMergeError := sheet.workbook.MergeCell(sheet.sheetName, cellAt(1, signatureRow), cellAt(lineFirstColumn-1, signatureRow))
			if labelMergeError != nil {
				return fmt.Errorf("failed to merge a signature label: %w", labelMergeError)
			}
		}
		labelError := sheet.writeCell(signatureRow, 1, signatureLine.label, sheetCellStyle{muted: true})
		if labelError != nil {
			return labelError
		}
		if lineFirstColumn < lastColumn {
			mergeError := sheet.workbook.MergeCell(sheet.sheetName, cellAt(lineFirstColumn, signatureRow), cellAt(lastColumn, signatureRow))
			if mergeError != nil {
				return fmt.Errorf("failed to merge a signature line: %w", mergeError)
			}
		}
		valueError := sheet.writeCell(signatureRow, lineFirstColumn, signatureLine.value, sheetCellStyle{headerEnd: true})
		for column := lineFirstColumn + 1; column <= lastColumn && valueError == nil; column++ {
			valueError = sheet.writeCell(signatureRow, column, nil, sheetCellStyle{headerEnd: true})
		}
		if valueError != nil {
			return valueError
		}
		heightError := sheet.workbook.SetRowHeight(sheet.sheetName, signatureRow, 26)
		if heightError != nil {
			return fmt.Errorf("failed to size a signature line: %w", heightError)
		}
		signatureRow++
	}
	return nil
}

func (sheet *documentSheet) writeSetup(headerRow int, isLandscape bool) error {
	gridLinesVisible := false
	viewError := sheet.workbook.SetSheetView(sheet.sheetName, 0, &excelize.ViewOptions{ShowGridLines: &gridLinesVisible})
	if viewError != nil {
		return fmt.Errorf("failed to hide the grid lines: %w", viewError)
	}
	return sheet.writePrintSetup(sheet.sheetName, headerRow, headerRow > 0, isLandscape)
}

func (sheet *documentSheet) writeMerged(sheetRow int, value string, style sheetCellStyle, lastColumn int) error {
	if lastColumn > 1 {
		mergeError := sheet.workbook.MergeCell(sheet.sheetName, cellAt(1, sheetRow), cellAt(lastColumn, sheetRow))
		if mergeError != nil {
			return fmt.Errorf("failed to merge a heading: %w", mergeError)
		}
	}
	return sheet.writeCell(sheetRow, 1, value, style)
}

func (sheet *documentSheet) writeCell(sheetRow int, column int, value any, style sheetCellStyle) error {
	cellName := cellAt(column, sheetRow)
	if value != nil {
		valueError := sheet.workbook.SetCellValue(sheet.sheetName, cellName, value)
		if valueError != nil {
			return fmt.Errorf("failed to write cell %s: %w", cellName, valueError)
		}
	}
	return sheet.applyStyle(cellName, style)
}

func (sheet *documentSheet) writeFormula(sheetRow int, column int, formula string, cachedValue any, style sheetCellStyle) error {
	cellName := cellAt(column, sheetRow)
	valueError := sheet.workbook.SetCellValue(sheet.sheetName, cellName, cachedValue)
	if valueError != nil {
		return fmt.Errorf("failed to write cell %s: %w", cellName, valueError)
	}
	formulaError := sheet.workbook.SetCellFormula(sheet.sheetName, cellName, formula)
	if formulaError != nil {
		return fmt.Errorf("failed to write the formula in %s: %w", cellName, formulaError)
	}
	return sheet.applyStyle(cellName, style)
}

func (sheet *documentSheet) applyStyle(cellName string, style sheetCellStyle) error {
	styleKey := fmt.Sprintf("%+v", style)
	styleId, isKnown := sheet.cellStyles[styleKey]
	if !isKnown {
		newStyleId, styleError := sheet.workbook.NewStyle(sheet.excelStyle(style))
		if styleError != nil {
			return fmt.Errorf("failed to create a cell style: %w", styleError)
		}
		sheet.cellStyles[styleKey] = newStyleId
		styleId = newStyleId
	}
	return sheet.workbook.SetCellStyle(sheet.sheetName, cellName, cellName, styleId)
}

func (sheet *documentSheet) excelStyle(style sheetCellStyle) *excelize.Style {
	fontSize := style.size
	if fontSize == 0 {
		fontSize = 10
	}
	fontColor := "1C2128"
	if style.muted {
		fontColor = excelMutedColor
	}
	brandColor := strings.TrimPrefix(firstNonEmpty(sheet.branding.PrimaryColor, defaultBrandColor), "#")
	if style.brand {
		fontColor = brandColor
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
		borders = append(borders, excelize.Border{Type: "bottom", Color: brandColor, Style: 2})
	}
	if style.rowRule {
		borders = append(borders, excelize.Border{Type: "bottom", Color: "E2E7ED", Style: 1})
	}
	excelStyle.Border = borders
	if style.format != "" {
		numberFormat := style.format
		excelStyle.CustomNumFmt = &numberFormat
	}
	return excelStyle
}

func (sheet *documentSheet) moneyFormat() string {
	if sheet.branding.CurrencyDecimals > 0 {
		decimalPart := "." + strings.Repeat("0", sheet.branding.CurrencyDecimals)
		return `#,##0` + decimalPart + `;(#,##0` + decimalPart + `);"–"`
	}
	return `#,##0;(#,##0);"–"`
}

func (sheet *documentSheet) majorUnits(minorUnits int64) any {
	if sheet.branding.CurrencyDecimals <= 0 {
		return minorUnits
	}
	return float64(minorUnits) / math.Pow10(sheet.branding.CurrencyDecimals)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
