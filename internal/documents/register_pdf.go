package documents

import (
	"fmt"
	"strings"

	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/page"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/border"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

const (
	registerRowHeight      = 6.2
	registerHeaderHeight   = 7.5
	registerFontSize       = 8.3
	registerSummaryWidth   = 25
	registerHeaderFontSize = 7.5
	registerHeaderLine     = 3.2
)

var registerRowRule = &props.Color{Red: 226, Green: 231, Blue: 237}

type registerPdfWriter struct {
	*pdfWriter
	register    Register
	columnSizes []int
}

func RegisterPdf(branding Branding, register Register) ([]byte, error) {
	baseWriter, writerError := newPdfWriter(branding, Document{
		Language:    register.Language,
		Title:       register.Title,
		Subtitle:    register.Subtitle,
		GeneratedBy: register.GeneratedBy,
		GeneratedAt: register.GeneratedAt,
		Filters:     register.Filters,
		Landscape:   register.Landscape,
	})
	if writerError != nil {
		return nil, writerError
	}
	writer := &registerPdfWriter{pdfWriter: baseWriter, register: register, columnSizes: registerColumnSizes(register.Columns)}
	return writer.render()
}

func (writer *registerPdfWriter) render() ([]byte, error) {
	headerRows := writer.letterheadRows()
	for _, headerRow := range headerRows {
		writer.headerHeights = append(writer.headerHeights, headerRow.GetHeight(nil, nil))
	}
	headerError := writer.maroto.RegisterHeader(headerRows...)
	if headerError != nil {
		return nil, fmt.Errorf("failed to add the register letterhead: %w", headerError)
	}
	writer.currentHeight = 0
	for _, headerHeight := range writer.headerHeights {
		writer.currentHeight += headerHeight
	}

	writer.addTitleBlock()
	writer.addSummary()
	writer.addRegisterHeader()
	writer.addOpeningRow()
	for _, registerRow := range writer.register.Rows {
		writer.addLine(registerRow)
	}
	if len(writer.register.Rows) == 0 && writer.register.EmptyText != "" {
		writer.add(text.NewRow(registerRowHeight+1, writer.register.EmptyText, props.Text{Size: registerFontSize, Style: fontstyle.Italic, Color: pdfMutedText, Top: 2, Left: pdfCellPadding}))
	}
	writer.addTotalsRow()
	writer.addClosingRow()
	writer.add(row.New(7))
	writer.keepClosingBlockTogether(writer.register.Notes)
	writer.addNumberedNotes(writer.register.Notes)
	writer.addSignatureLines(writer.register.GeneratedBy)

	generatedDocument, generateError := writer.maroto.Generate()
	if generateError != nil {
		return nil, fmt.Errorf("failed to build the register PDF: %w", generateError)
	}
	return generatedDocument.GetBytes(), nil
}

func (writer *registerPdfWriter) addSummary() {
	if len(writer.register.Summary) == 0 {
		return
	}
	cellStyle := &props.Cell{BorderType: border.Top, BorderColor: statementRuleColor, BorderThickness: 0.25}
	summaryRow := row.New(13)
	for _, summaryField := range writer.register.Summary[:min(len(writer.register.Summary), pdfGridSize/registerSummaryWidth)] {
		fieldValue := FormatValue(writer.branding, writer.language, summaryField.Kind, summaryField.Value)
		if summaryField.Kind == Money {
			fieldValue = StatementAmount(moneyOf(summaryField.Value), writer.branding.CurrencyDecimals)
		}
		valueStyle := fontstyle.Normal
		if summaryField.Strong {
			valueStyle = fontstyle.Bold
		}
		summaryRow.Add(col.New(registerSummaryWidth).Add(
			text.New(summaryField.Label, props.Text{Size: 7.5, Color: pdfMutedText, Top: 1.6, Left: pdfCellPadding}),
			text.New(fieldValue, props.Text{Size: 10.5, Style: valueStyle, Top: 6, Left: pdfCellPadding}),
		).WithStyle(cellStyle))
	}
	writer.add(summaryRow)
	writer.add(row.New(5))
}

func (writer *registerPdfWriter) addRegisterHeader() {
	headerLines := make([][]string, len(writer.register.Columns))
	lineCount := 1
	for columnIndex, column := range writer.register.Columns {
		titleWidth := writer.columnWidth(writer.columnSizes[columnIndex]) - 2*pdfCellPadding
		wrappedTitle := wrapToWidthAtBold(column.Title, titleWidth, registerHeaderFontSize)
		if len(wrappedTitle) > 2 {
			wrappedTitle = []string{wrappedTitle[0], truncateToWidth(strings.Join(wrappedTitle[1:], " "), titleWidth, registerHeaderFontSize)}
		}
		headerLines[columnIndex] = wrappedTitle
		lineCount = max(lineCount, len(wrappedTitle))
	}
	headerHeight := registerHeaderHeight + float64(lineCount-1)*registerHeaderLine
	headerRow := row.New(headerHeight)
	for columnIndex, column := range writer.register.Columns {
		titleComponents := []core.Component{}
		titleTop := 2.3 + float64(lineCount-len(headerLines[columnIndex]))*registerHeaderLine
		for lineIndex, titleLine := range headerLines[columnIndex] {
			titleComponents = append(titleComponents, text.New(titleLine, props.Text{
				Size:  registerHeaderFontSize,
				Style: fontstyle.Bold,
				Top:   titleTop + float64(lineIndex)*registerHeaderLine,
				Left:  pdfCellPadding,
				Right: pdfCellPadding,
				Align: alignmentFor(column.Kind),
			}))
		}
		headerRow.Add(col.New(writer.columnSizes[columnIndex]).Add(titleComponents...))
	}
	headerRow.WithStyle(&props.Cell{BorderType: border.Bottom, BorderColor: writer.brandColor, BorderThickness: 0.5})
	writer.add(headerRow)
	writer.add(row.New(0.8))
}

func wrapToWidthAtBold(value string, widthMillimeters float64, fontSize float64) []string {
	return wrapToWidth(value, widthMillimeters*pdfAverageCharEm/pdfBoldCharEm, fontSize)
}

func (writer *registerPdfWriter) addHeaderOnNewPageIfNeeded(height float64) {
	if writer.fits(height) {
		return
	}
	writer.addColumnHeadingsPage()
}

func (writer *registerPdfWriter) addColumnHeadingsPage() {
	headerOnlyHeight := 0.0
	for _, headerHeight := range writer.headerHeights {
		headerOnlyHeight += headerHeight
	}
	if writer.currentHeight == headerOnlyHeight {
		return
	}
	writer.maroto.AddPages(page.New())
	writer.startNewPage()
	writer.addRegisterHeader()
}

func (writer *registerPdfWriter) addOpeningRow() {
	if writer.register.OpeningBalance == nil {
		return
	}
	writer.add(writer.labelledBalanceRow(writer.register.OpeningLabel, *writer.register.OpeningBalance, fontstyle.Italic, nil))
}

func (writer *registerPdfWriter) addLine(registerRow RegisterRow) {
	writer.addHeaderOnNewPageIfNeeded(registerRowHeight)
	lineRow := row.New(registerRowHeight)
	for columnIndex, column := range writer.register.Columns {
		lineRow.Add(writer.cell(column.Kind, cellOf(registerRow, columnIndex), writer.columnSizes[columnIndex], fontstyle.Normal))
	}
	lineRow.WithStyle(&props.Cell{BorderType: border.Bottom, BorderColor: registerRowRule, BorderThickness: 0.15})
	writer.add(lineRow)
}

func (writer *registerPdfWriter) addTotalsRow() {
	if !writer.register.hasSums() || len(writer.register.Rows) == 0 {
		return
	}
	writer.addHeaderOnNewPageIfNeeded(registerRowHeight + 1)
	firstSumIndex := 0
	for firstSumIndex < len(writer.register.Columns) && !writer.register.Columns[firstSumIndex].Sum {
		firstSumIndex++
	}
	labelSize := 0
	for columnIndex := range firstSumIndex {
		labelSize += writer.columnSizes[columnIndex]
	}
	totalsRow := row.New(registerRowHeight + 1)
	if labelSize > 0 {
		totalsRow.Add(text.NewCol(labelSize, writer.register.TotalLabel, props.Text{Size: registerFontSize, Style: fontstyle.Bold, Top: pdfTextTop, Left: pdfCellPadding}))
	}
	for columnIndex := firstSumIndex; columnIndex < len(writer.register.Columns); columnIndex++ {
		column := writer.register.Columns[columnIndex]
		columnSize := writer.columnSizes[columnIndex]
		if column.Sum {
			totalsRow.Add(writer.cell(column.Kind, writer.register.columnTotal(columnIndex), columnSize, fontstyle.Bold).WithStyle(&props.Cell{BorderType: border.Top, BorderColor: statementRuleColor, BorderThickness: 0.3}))
			continue
		}
		totalsRow.Add(col.New(columnSize))
	}
	writer.add(totalsRow)
}

func (writer *registerPdfWriter) addClosingRow() {
	if writer.register.balanceColumn() < 0 {
		return
	}
	writer.addHeaderOnNewPageIfNeeded(registerRowHeight + 3)
	closingCell := &props.Cell{BackgroundColor: statementTotalFill, BorderType: border.Top | border.Bottom, BorderColor: statementRuleColor, BorderThickness: 0.35}
	writer.add(row.New(1))
	writer.add(writer.labelledBalanceRow(writer.register.ClosingLabel, writer.register.closingBalance(), fontstyle.Bold, closingCell))
	balanceIndex := writer.register.balanceColumn()
	ruleRow := row.New(0.9)
	for columnIndex := range writer.register.Columns {
		ruleColumn := col.New(writer.columnSizes[columnIndex])
		if columnIndex == balanceIndex {
			ruleColumn.WithStyle(&props.Cell{BorderType: border.Bottom, BorderColor: statementRuleColor, BorderThickness: 0.35})
		}
		ruleRow.Add(ruleColumn)
	}
	writer.add(ruleRow)
}

func (writer *registerPdfWriter) labelledBalanceRow(label string, balance int64, style fontstyle.Type, balanceCell *props.Cell) core.Row {
	balanceIndex := writer.register.balanceColumn()
	labelSize := 0
	for columnIndex := range writer.register.Columns {
		if columnIndex < balanceIndex {
			labelSize += writer.columnSizes[columnIndex]
		}
	}
	balanceRow := row.New(registerRowHeight + 0.5)
	labelCell := &props.Cell{}
	if balanceCell != nil {
		labelCell = &props.Cell{BackgroundColor: balanceCell.BackgroundColor}
	}
	if labelSize > 0 {
		balanceRow.Add(text.NewCol(labelSize, label, props.Text{Size: registerFontSize, Style: style, Top: pdfTextTop, Left: pdfCellPadding}).WithStyle(labelCell))
	}
	balanceRow.Add(text.NewCol(writer.columnSizes[balanceIndex], StatementAmount(balance, writer.branding.CurrencyDecimals), props.Text{
		Size: registerFontSize, Style: style, Top: pdfTextTop, Right: pdfCellPadding, Align: align.Right,
	}).WithStyle(balanceCell))
	for columnIndex := balanceIndex + 1; columnIndex < len(writer.register.Columns); columnIndex++ {
		balanceRow.Add(col.New(writer.columnSizes[columnIndex]))
	}
	return balanceRow
}

func (writer *registerPdfWriter) cell(kind Kind, cellValue any, columnSize int, style fontstyle.Type) core.Col {
	cellText := FormatValue(writer.branding, writer.language, kind, cellValue)
	if kind == Money {
		cellAmount, isAmount := asInteger(cellValue)
		cellText = ""
		if isAmount {
			cellText = StatementAmount(cellAmount, writer.branding.CurrencyDecimals)
		}
	}
	return text.NewCol(columnSize, truncateToWidth(cellText, writer.columnWidth(columnSize)-2*pdfCellPadding, registerFontSize), props.Text{
		Size:  registerFontSize,
		Style: style,
		Top:   pdfTextTop,
		Left:  pdfCellPadding,
		Right: pdfCellPadding,
		Align: alignmentFor(kind),
	})
}

func registerColumnSizes(columns []RegisterColumn) []int {
	if len(columns) == 0 {
		return nil
	}
	columnWeights := make([]float64, len(columns))
	totalWeight := 0.0
	isFirstText := true
	for columnIndex, column := range columns {
		columnWeight := column.Weight
		if columnWeight == 0 {
			switch column.Kind {
			case Text:
				columnWeight = 2
				if isFirstText {
					columnWeight = 3.4
				}
			case Date:
				columnWeight = 1.5
			case DateTime:
				columnWeight = 2.1
			case Integer, Percent:
				columnWeight = 1.1
			default:
				columnWeight = 1.7
			}
		}
		if column.Kind == Text {
			isFirstText = false
		}
		columnWeights[columnIndex] = columnWeight
		totalWeight += columnWeight
	}
	columnSizes := make([]int, len(columns))
	usedSize := 0
	for columnIndex := range columns {
		columnSizes[columnIndex] = max(int(columnWeights[columnIndex]/totalWeight*pdfGridSize), 1)
		usedSize += columnSizes[columnIndex]
	}
	widestColumn := 0
	for columnIndex := range columns {
		if columnWeights[columnIndex] > columnWeights[widestColumn] {
			widestColumn = columnIndex
		}
	}
	columnSizes[widestColumn] += pdfGridSize - usedSize
	return columnSizes
}
