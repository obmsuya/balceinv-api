package documents

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	excelLogoHeightPixels = 48.0
	excelLogoWidthPixels  = 180.0
	excelMinimumWidth     = 9.0
	excelMaximumWidth     = 55.0
	excelSheetNameLimit   = 31
	excelWideTableColumns = 6
	excelBorderColor      = "D5DBE3"
	excelZebraColor       = "F3F6F9"
	excelTotalsColor      = "E7ECF2"
	excelMutedColor       = "5B6573"
)

type excelStyleKey struct {
	kind    Kind
	variant string
}

type excelSheetTable struct {
	title      string
	columns    []Column
	rows       [][]any
	valueKinds []Kind
	fields     []Field
	notes      []string
	landscape  bool
}

type excelHeaderLine struct {
	text    string
	variant string
	height  float64
}

type excelWriter struct {
	workbook   *excelize.File
	branding   Branding
	document   Document
	language   string
	styles     map[excelStyleKey]int
	sheetNames map[string]bool
}

func Excel(branding Branding, document Document) ([]byte, error) {
	workbook := excelize.NewFile()
	defer workbook.Close()

	writer := &excelWriter{
		workbook:   workbook,
		branding:   branding,
		document:   document,
		language:   document.language(),
		styles:     map[excelStyleKey]int{},
		sheetNames: map[string]bool{},
	}

	sheetTables := writer.sheetTables()
	for sheetIndex, sheetTable := range sheetTables {
		sheetName := writer.uniqueSheetName(sheetTable.title)
		if sheetIndex == 0 {
			renameError := workbook.SetSheetName(workbook.GetSheetName(0), sheetName)
			if renameError != nil {
				return nil, fmt.Errorf("failed to name the first sheet: %w", renameError)
			}
		} else {
			_, newSheetError := workbook.NewSheet(sheetName)
			if newSheetError != nil {
				return nil, fmt.Errorf("failed to add sheet %q: %w", sheetName, newSheetError)
			}
		}
		sheetError := writer.writeSheet(sheetName, sheetTable)
		if sheetError != nil {
			return nil, sheetError
		}
	}
	workbook.SetActiveSheet(0)

	propertiesError := writer.writeProperties()
	if propertiesError != nil {
		return nil, propertiesError
	}

	workbookBuffer, writeError := workbook.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("failed to write the workbook: %w", writeError)
	}
	return workbookBuffer.Bytes(), nil
}

func (writer *excelWriter) sheetTables() []excelSheetTable {
	sheetTables := []excelSheetTable{}
	if len(writer.document.Cards) > 0 {
		cardRows := [][]any{}
		cardKinds := []Kind{}
		for _, card := range writer.document.Cards {
			cardRows = append(cardRows, []any{card.Label, card.Value})
			cardKinds = append(cardKinds, card.Kind)
		}
		sheetTables = append(sheetTables, excelSheetTable{
			title:      Label(writer.language, "summary"),
			columns:    []Column{{Title: Label(writer.language, "item"), Kind: Text}, {Title: Label(writer.language, "value"), Kind: Text}},
			rows:       cardRows,
			valueKinds: cardKinds,
		})
	}
	for _, table := range writer.document.Tables {
		sheetTables = append(sheetTables, excelSheetTable{
			title:     table.Title,
			columns:   table.Columns,
			rows:      table.Rows,
			landscape: writer.document.Landscape || len(table.Columns) > excelWideTableColumns,
		})
	}
	if len(sheetTables) == 0 {
		sheetTables = append(sheetTables, excelSheetTable{title: writer.document.Title})
	}
	lastSheet := &sheetTables[len(sheetTables)-1]
	lastSheet.fields = writer.document.Totals
	lastSheet.notes = writer.document.Notes

	if len(writer.document.Filters) > 0 {
		filterRows := [][]any{}
		filterKinds := []Kind{}
		for _, filter := range writer.document.Filters {
			filterRows = append(filterRows, []any{filter.Label, filter.Value})
			filterKinds = append(filterKinds, filter.Kind)
		}
		sheetTables = append(sheetTables, excelSheetTable{
			title:      Label(writer.language, "filters"),
			columns:    []Column{{Title: Label(writer.language, "filter"), Kind: Text}, {Title: Label(writer.language, "value"), Kind: Text}},
			rows:       filterRows,
			valueKinds: filterKinds,
		})
	}
	return sheetTables
}

func (writer *excelWriter) uniqueSheetName(rawName string) string {
	cleanName := strings.Map(func(character rune) rune {
		if strings.ContainsRune(`[]:*?/\`, character) {
			return '-'
		}
		return character
	}, strings.TrimSpace(rawName))
	cleanName = strings.Trim(cleanName, "'")
	if cleanName == "" {
		cleanName = Label(writer.language, "summary")
	}
	cleanName = truncateRunes(cleanName, excelSheetNameLimit)

	candidateName := cleanName
	for suffix := 2; writer.sheetNames[strings.ToLower(candidateName)]; suffix++ {
		suffixText := fmt.Sprintf(" (%d)", suffix)
		candidateName = truncateRunes(cleanName, excelSheetNameLimit-len(suffixText)) + suffixText
	}
	writer.sheetNames[strings.ToLower(candidateName)] = true
	return candidateName
}

func (writer *excelWriter) writeSheet(sheetName string, sheetTable excelSheetTable) error {
	columnCount := max(len(sheetTable.columns), 2)
	headerRow, headerBlockError := writer.writeHeaderBlock(sheetName, columnCount)
	if headerBlockError != nil {
		return headerBlockError
	}

	lastRow := headerRow - 1
	if len(sheetTable.columns) > 0 {
		tableLastRow, tableError := writer.writeTable(sheetName, headerRow, sheetTable)
		if tableError != nil {
			return tableError
		}
		lastRow = tableLastRow
	}

	fieldsError := writer.writeFieldsAndNotes(sheetName, lastRow+2, columnCount, sheetTable)
	if fieldsError != nil {
		return fieldsError
	}
	return writer.writePrintSetup(sheetName, headerRow, len(sheetTable.columns) > 0, sheetTable.landscape)
}

func (writer *excelWriter) writeHeaderBlock(sheetName string, columnCount int) (int, error) {
	headerLines := []excelHeaderLine{
		{writer.branding.CompanyName, "company", 26},
		{writer.branding.taxLine(writer.language), "muted", 0},
		{writer.branding.contactLine(), "muted", 0},
		{"", "", 0},
		{writer.documentTitleText(), "title", 22},
		{writer.document.Subtitle, "period", 0},
	}
	for _, detail := range writer.document.Details {
		headerLines = append(headerLines, excelHeaderLine{detail.Label + ": " + FormatValue(writer.branding, writer.language, detail.Kind, detail.Value), "plain", 0})
	}
	headerLines = append(headerLines, excelHeaderLine{writer.generatedLine(), "muted", 0})

	currentRow := 1
	for _, headerLine := range headerLines {
		if headerLine.text == "" && headerLine.variant != "" {
			continue
		}
		if headerLine.text != "" {
			cellName := cellAt(1, currentRow)
			setError := writer.workbook.SetCellValue(sheetName, cellName, headerLine.text)
			if setError != nil {
				return 0, fmt.Errorf("failed to write the document header: %w", setError)
			}
			styleId, styleError := writer.style(Text, headerLine.variant)
			if styleError != nil {
				return 0, styleError
			}
			styleSetError := writer.workbook.SetCellStyle(sheetName, cellName, cellName, styleId)
			if styleSetError != nil {
				return 0, fmt.Errorf("failed to style the document header: %w", styleSetError)
			}
		}
		if headerLine.height > 0 {
			heightError := writer.workbook.SetRowHeight(sheetName, currentRow, headerLine.height)
			if heightError != nil {
				return 0, fmt.Errorf("failed to size the document header: %w", heightError)
			}
		}
		currentRow++
	}

	logoError := writer.addLogo(sheetName, columnCount)
	if logoError != nil {
		return 0, logoError
	}
	return currentRow + 1, nil
}

func (writer *excelWriter) documentTitleText() string {
	if writer.document.Number == "" {
		return writer.document.Title
	}
	return writer.document.Title + "  ·  " + Label(writer.language, "number") + " " + writer.document.Number
}

func (writer *excelWriter) generatedLine() string {
	generatedAt := writer.document.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return Label(writer.language, "generatedBy", "{user}", nameOrBalce(writer.document.GeneratedBy), "{date}", FormatDateTime(writer.language, generatedAt.In(writer.branding.Location())))
}

func (writer *excelWriter) addLogo(sheetName string, columnCount int) error {
	if len(writer.branding.LogoPng) == 0 || writer.branding.LogoHeight == 0 || writer.branding.LogoWidth == 0 {
		return nil
	}
	logoScale := math.Min(excelLogoHeightPixels/float64(writer.branding.LogoHeight), excelLogoWidthPixels/float64(writer.branding.LogoWidth))
	logoPicture := &excelize.Picture{
		Extension: ".png",
		File:      writer.branding.LogoPng,
		Format: &excelize.GraphicOptions{
			AltText:         writer.branding.CompanyName,
			ScaleX:          logoScale,
			ScaleY:          logoScale,
			LockAspectRatio: true,
			Positioning:     "oneCell",
		},
	}
	pictureError := writer.workbook.AddPictureFromBytes(sheetName, cellAt(columnCount, 1), logoPicture)
	if pictureError != nil {
		return fmt.Errorf("failed to add the logo: %w", pictureError)
	}
	return nil
}

func (writer *excelWriter) writeTable(sheetName string, headerRow int, sheetTable excelSheetTable) (int, error) {
	for columnIndex, column := range sheetTable.columns {
		setError := writer.workbook.SetCellValue(sheetName, cellAt(columnIndex+1, headerRow), column.Title)
		if setError != nil {
			return 0, fmt.Errorf("failed to write the table header: %w", setError)
		}
	}
	headerStyle, headerStyleError := writer.style(Text, "header")
	if headerStyleError != nil {
		return 0, headerStyleError
	}
	lastColumn := len(sheetTable.columns)
	headerStyleSetError := writer.workbook.SetCellStyle(sheetName, cellAt(1, headerRow), cellAt(lastColumn, headerRow), headerStyle)
	if headerStyleSetError != nil {
		return 0, fmt.Errorf("failed to style the table header: %w", headerStyleSetError)
	}
	headerHeightError := writer.workbook.SetRowHeight(sheetName, headerRow, 22)
	if headerHeightError != nil {
		return 0, fmt.Errorf("failed to size the table header: %w", headerHeightError)
	}

	columnWidths := make([]float64, lastColumn)
	for columnIndex, column := range sheetTable.columns {
		columnWidths[columnIndex] = float64(utf8.RuneCountInString(column.Title)) + 3
	}

	firstDataRow := headerRow + 1
	if len(sheetTable.rows) == 0 {
		emptyCell := cellAt(1, firstDataRow)
		setError := writer.workbook.SetCellValue(sheetName, emptyCell, Label(writer.language, "nothingToShow"))
		if setError != nil {
			return 0, fmt.Errorf("failed to write an empty table: %w", setError)
		}
		mutedStyle, mutedStyleError := writer.style(Text, "muted")
		if mutedStyleError != nil {
			return 0, mutedStyleError
		}
		mutedSetError := writer.workbook.SetCellStyle(sheetName, emptyCell, emptyCell, mutedStyle)
		if mutedSetError != nil {
			return 0, fmt.Errorf("failed to style an empty table: %w", mutedSetError)
		}
		return firstDataRow, writer.applyColumnWidths(sheetName, columnWidths)
	}

	for rowIndex, rowValues := range sheetTable.rows {
		rowNumber := firstDataRow + rowIndex
		rowVariant := "data"
		if rowIndex%2 == 1 {
			rowVariant = "zebra"
		}
		for columnIndex, column := range sheetTable.columns {
			cellKind := column.Kind
			if sheetTable.valueKinds != nil && columnIndex == lastColumn-1 {
				cellKind = sheetTable.valueKinds[rowIndex]
			}
			var cellValue any
			if columnIndex < len(rowValues) {
				cellValue = rowValues[columnIndex]
			}
			cellName := cellAt(columnIndex+1, rowNumber)
			writeError := writer.writeCell(sheetName, cellName, cellKind, cellValue, rowVariant)
			if writeError != nil {
				return 0, writeError
			}
			displayWidth := float64(utf8.RuneCountInString(FormatValue(writer.branding, writer.language, cellKind, cellValue))) + 3
			columnWidths[columnIndex] = math.Max(columnWidths[columnIndex], displayWidth)
		}
	}
	lastDataRow := firstDataRow + len(sheetTable.rows) - 1

	filterError := writer.workbook.AutoFilter(sheetName, cellAt(1, headerRow)+":"+cellAt(lastColumn, lastDataRow), nil)
	if filterError != nil {
		return 0, fmt.Errorf("failed to add the filter buttons: %w", filterError)
	}

	totalsRow, totalsError := writer.writeTotalsRow(sheetName, sheetTable.columns, firstDataRow, lastDataRow)
	if totalsError != nil {
		return 0, totalsError
	}
	return totalsRow, writer.applyColumnWidths(sheetName, columnWidths)
}

func (writer *excelWriter) writeTotalsRow(sheetName string, columns []Column, firstDataRow int, lastDataRow int) (int, error) {
	hasSums := false
	for _, column := range columns {
		hasSums = hasSums || column.Sum
	}
	if !hasSums {
		return lastDataRow, nil
	}

	totalsRow := lastDataRow + 1
	for columnIndex, column := range columns {
		cellName := cellAt(columnIndex+1, totalsRow)
		styleKind := column.Kind
		if column.Sum {
			columnName, columnNameError := excelize.ColumnNumberToName(columnIndex + 1)
			if columnNameError != nil {
				return 0, fmt.Errorf("failed to name a totals column: %w", columnNameError)
			}
			sumFormula := fmt.Sprintf("SUM(%s%d:%s%d)", columnName, firstDataRow, columnName, lastDataRow)
			formulaError := writer.workbook.SetCellFormula(sheetName, cellName, sumFormula)
			if formulaError != nil {
				return 0, fmt.Errorf("failed to write a totals formula: %w", formulaError)
			}
		} else if columnIndex == 0 {
			styleKind = Text
			setError := writer.workbook.SetCellValue(sheetName, cellName, Label(writer.language, "total"))
			if setError != nil {
				return 0, fmt.Errorf("failed to write the totals label: %w", setError)
			}
		}
		totalsStyle, totalsStyleError := writer.style(styleKind, "totals")
		if totalsStyleError != nil {
			return 0, totalsStyleError
		}
		styleSetError := writer.workbook.SetCellStyle(sheetName, cellName, cellName, totalsStyle)
		if styleSetError != nil {
			return 0, fmt.Errorf("failed to style the totals row: %w", styleSetError)
		}
	}
	return totalsRow, nil
}

func (writer *excelWriter) writeCell(sheetName string, cellName string, cellKind Kind, cellValue any, rowVariant string) error {
	storedValue, storedKind := writer.excelValue(cellKind, cellValue)
	if storedValue != nil {
		setError := writer.workbook.SetCellValue(sheetName, cellName, storedValue)
		if setError != nil {
			return fmt.Errorf("failed to write cell %s: %w", cellName, setError)
		}
	}
	cellStyle, styleError := writer.style(storedKind, rowVariant)
	if styleError != nil {
		return styleError
	}
	styleSetError := writer.workbook.SetCellStyle(sheetName, cellName, cellName, cellStyle)
	if styleSetError != nil {
		return fmt.Errorf("failed to style cell %s: %w", cellName, styleSetError)
	}
	return nil
}

func (writer *excelWriter) excelValue(cellKind Kind, cellValue any) (any, Kind) {
	switch typedValue := cellValue.(type) {
	case nil:
		return nil, cellKind
	case string:
		return typedValue, Text
	case *time.Time:
		if typedValue == nil {
			return nil, cellKind
		}
		return writer.excelValue(cellKind, *typedValue)
	case time.Time:
		localMoment := typedValue.In(writer.branding.Location())
		wallClock := time.Date(localMoment.Year(), localMoment.Month(), localMoment.Day(), localMoment.Hour(), localMoment.Minute(), localMoment.Second(), 0, time.UTC)
		if cellKind != DateTime {
			return time.Date(wallClock.Year(), wallClock.Month(), wallClock.Day(), 0, 0, 0, 0, time.UTC), Date
		}
		return wallClock, DateTime
	}

	integerValue, isInteger := asInteger(cellValue)
	if !isInteger {
		return fmt.Sprint(cellValue), Text
	}
	switch cellKind {
	case Money:
		return float64(integerValue) / math.Pow10(writer.branding.CurrencyDecimals), Money
	case Percent:
		return float64(integerValue) / 10000, Percent
	default:
		return integerValue, Integer
	}
}

func (writer *excelWriter) writeFieldsAndNotes(sheetName string, startRow int, columnCount int, sheetTable excelSheetTable) error {
	currentRow := startRow
	labelColumn := max(columnCount-1, 1)
	for _, field := range sheetTable.fields {
		labelCell := cellAt(labelColumn, currentRow)
		labelError := writer.workbook.SetCellValue(sheetName, labelCell, field.Label)
		if labelError != nil {
			return fmt.Errorf("failed to write a total label: %w", labelError)
		}
		labelVariant := "fieldLabel"
		valueVariant := "field"
		if field.Strong {
			labelVariant = "strongLabel"
			valueVariant = "strong"
		}
		labelStyle, labelStyleError := writer.style(Text, labelVariant)
		if labelStyleError != nil {
			return labelStyleError
		}
		labelStyleSetError := writer.workbook.SetCellStyle(sheetName, labelCell, labelCell, labelStyle)
		if labelStyleSetError != nil {
			return fmt.Errorf("failed to style a total label: %w", labelStyleSetError)
		}
		valueError := writer.writeCell(sheetName, cellAt(labelColumn+1, currentRow), field.Kind, field.Value, valueVariant)
		if valueError != nil {
			return valueError
		}
		currentRow++
	}
	if len(sheetTable.fields) > 0 {
		currentRow++
	}
	for _, note := range sheetTable.notes {
		for _, noteLine := range strings.Split(note, "\n") {
			if strings.TrimSpace(noteLine) == "" {
				continue
			}
			noteError := writer.workbook.SetCellValue(sheetName, cellAt(1, currentRow), strings.TrimSpace(noteLine))
			if noteError != nil {
				return fmt.Errorf("failed to write a note: %w", noteError)
			}
			currentRow++
		}
	}
	return nil
}

func (writer *excelWriter) applyColumnWidths(sheetName string, columnWidths []float64) error {
	for columnIndex, columnWidth := range columnWidths {
		columnName, columnNameError := excelize.ColumnNumberToName(columnIndex + 1)
		if columnNameError != nil {
			return fmt.Errorf("failed to name a column: %w", columnNameError)
		}
		clampedWidth := math.Min(math.Max(columnWidth, excelMinimumWidth), excelMaximumWidth)
		widthError := writer.workbook.SetColWidth(sheetName, columnName, columnName, clampedWidth)
		if widthError != nil {
			return fmt.Errorf("failed to size a column: %w", widthError)
		}
	}
	return nil
}

func (writer *excelWriter) writePrintSetup(sheetName string, headerRow int, hasTable bool, isLandscape bool) error {
	if hasTable {
		panesError := writer.workbook.SetPanes(sheetName, &excelize.Panes{
			Freeze:      true,
			YSplit:      headerRow,
			TopLeftCell: cellAt(1, headerRow+1),
			ActivePane:  "bottomLeft",
		})
		if panesError != nil {
			return fmt.Errorf("failed to freeze the table header: %w", panesError)
		}
		quotedSheetName := "'" + strings.ReplaceAll(sheetName, "'", "''") + "'"
		titlesError := writer.workbook.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Titles",
			RefersTo: fmt.Sprintf("%s!$%d:$%d", quotedSheetName, headerRow, headerRow),
			Scope:    sheetName,
		})
		if titlesError != nil {
			return fmt.Errorf("failed to repeat the table header when printing: %w", titlesError)
		}
	}

	pageOrientation := "portrait"
	if isLandscape {
		pageOrientation = "landscape"
	}
	paperSizeA4 := 9
	fitOnePageWide := 1
	anyNumberOfPagesTall := 0
	layoutError := writer.workbook.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &paperSizeA4,
		Orientation: &pageOrientation,
		FitToWidth:  &fitOnePageWide,
		FitToHeight: &anyNumberOfPagesTall,
	})
	if layoutError != nil {
		return fmt.Errorf("failed to set up printing: %w", layoutError)
	}
	fitToPage := true
	propsError := writer.workbook.SetSheetProps(sheetName, &excelize.SheetPropsOptions{FitToPage: &fitToPage})
	if propsError != nil {
		return fmt.Errorf("failed to fit the sheet to the page: %w", propsError)
	}

	escapedCompany := strings.ReplaceAll(writer.branding.CompanyName, "&", "&&")
	footerError := writer.workbook.SetHeaderFooter(sheetName, &excelize.HeaderFooterOptions{
		OddFooter: "&L&8" + escapedCompany + "&R&8" + Label(writer.language, "excelFooter"),
	})
	if footerError != nil {
		return fmt.Errorf("failed to add the page footer: %w", footerError)
	}
	return nil
}

func (writer *excelWriter) writeProperties() error {
	generatedAt := writer.document.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	docPropsError := writer.workbook.SetDocProps(&excelize.DocProperties{
		Title:          writer.document.Title,
		Subject:        writer.document.Subtitle,
		Creator:        "Balce",
		LastModifiedBy: nameOrBalce(writer.document.GeneratedBy),
		Created:        generatedAt.UTC().Format(time.RFC3339),
		Modified:       generatedAt.UTC().Format(time.RFC3339),
		Language:       writer.language,
		Description:    writer.generatedLine(),
	})
	if docPropsError != nil {
		return fmt.Errorf("failed to set the workbook properties: %w", docPropsError)
	}
	appPropsError := writer.workbook.SetAppProps(&excelize.AppProperties{
		Application: "Balce",
		Company:     writer.branding.CompanyName,
	})
	if appPropsError != nil {
		return fmt.Errorf("failed to set the workbook company: %w", appPropsError)
	}
	return nil
}

func (writer *excelWriter) style(kind Kind, variant string) (int, error) {
	styleKey := excelStyleKey{kind: kind, variant: variant}
	existingStyle, isKnown := writer.styles[styleKey]
	if isKnown {
		return existingStyle, nil
	}

	brandColor := strings.TrimPrefix(writer.branding.PrimaryColor, "#")
	thinBorders := []excelize.Border{
		{Type: "left", Color: excelBorderColor, Style: 1},
		{Type: "right", Color: excelBorderColor, Style: 1},
		{Type: "top", Color: excelBorderColor, Style: 1},
		{Type: "bottom", Color: excelBorderColor, Style: 1},
	}
	cellStyle := &excelize.Style{}
	switch variant {
	case "company":
		cellStyle.Font = &excelize.Font{Bold: true, Size: 16, Color: brandColor}
	case "title":
		cellStyle.Font = &excelize.Font{Bold: true, Size: 13}
	case "period":
		cellStyle.Font = &excelize.Font{Size: 10, Color: excelMutedColor}
	case "muted":
		cellStyle.Font = &excelize.Font{Size: 9, Color: excelMutedColor}
	case "plain":
		cellStyle.Font = &excelize.Font{Size: 10}
	case "header":
		cellStyle.Font = &excelize.Font{Bold: true, Color: "FFFFFF"}
		cellStyle.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{brandColor}}
		cellStyle.Border = thinBorders
		cellStyle.Alignment = &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	case "data", "zebra":
		cellStyle.Border = thinBorders
		if variant == "zebra" {
			cellStyle.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{excelZebraColor}}
		}
	case "totals":
		cellStyle.Font = &excelize.Font{Bold: true}
		cellStyle.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{excelTotalsColor}}
		cellStyle.Border = []excelize.Border{
			{Type: "left", Color: excelBorderColor, Style: 1},
			{Type: "right", Color: excelBorderColor, Style: 1},
			{Type: "top", Color: brandColor, Style: 2},
			{Type: "bottom", Color: brandColor, Style: 6},
		}
	case "fieldLabel", "strongLabel":
		cellStyle.Font = &excelize.Font{Bold: variant == "strongLabel"}
		cellStyle.Alignment = &excelize.Alignment{Horizontal: "right"}
	case "field":
	case "strong":
		cellStyle.Font = &excelize.Font{Bold: true, Size: 12}
	}

	numberFormat := writer.numberFormat(kind)
	if numberFormat != "" {
		cellStyle.CustomNumFmt = &numberFormat
	}
	if kind == Money || kind == Integer || kind == Percent {
		if cellStyle.Alignment == nil {
			cellStyle.Alignment = &excelize.Alignment{}
		}
		if variant != "header" {
			cellStyle.Alignment.Horizontal = "right"
		}
	}

	styleId, styleError := writer.workbook.NewStyle(cellStyle)
	if styleError != nil {
		return 0, fmt.Errorf("failed to create a cell style: %w", styleError)
	}
	writer.styles[styleKey] = styleId
	return styleId, nil
}

func (writer *excelWriter) numberFormat(kind Kind) string {
	switch kind {
	case Money:
		if writer.branding.CurrencyDecimals > 0 {
			decimalZeros := strings.Repeat("0", writer.branding.CurrencyDecimals)
			return "#,##0." + decimalZeros + ";[Red]-#,##0." + decimalZeros
		}
		return "#,##0;[Red]-#,##0"
	case Integer:
		return "#,##0"
	case Percent:
		return "0.0%"
	case Date:
		return "d mmm yyyy"
	case DateTime:
		return "d mmm yyyy hh:mm"
	default:
		return ""
	}
}

func cellAt(columnNumber int, rowNumber int) string {
	cellName, coordinatesError := excelize.CoordinatesToCellName(columnNumber, rowNumber)
	if coordinatesError != nil {
		return "A1"
	}
	return cellName
}

func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

func nameOrBalce(userName string) string {
	if strings.TrimSpace(userName) == "" {
		return "Balce"
	}
	return userName
}
