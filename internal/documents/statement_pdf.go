package documents

import (
	"fmt"
	"strconv"
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
	statementLabelColumns  = 58
	statementAmountColumns = 15
	statementChangeColumns = 12
	statementItemIndent    = 5.0
	statementCodeWidth     = 11.0
	statementFontSize      = 9.0
	statementNoteFontSize  = 8.0
	statementNoteLine      = 4.2
)

var (
	statementRuleColor = &props.Color{Red: 60, Green: 68, Blue: 80}
	statementTotalFill = &props.Color{Red: 241, Green: 244, Blue: 247}
)

type statementPdfWriter struct {
	*pdfWriter
	statement      Statement
	currentSection string
}

func StatementPdf(branding Branding, statement Statement) ([]byte, error) {
	baseWriter, writerError := newPdfWriter(branding, Document{
		Language:    statement.Language,
		Title:       statement.Title,
		Subtitle:    statement.Subtitle,
		GeneratedBy: statement.GeneratedBy,
		GeneratedAt: statement.GeneratedAt,
		Filters:     statement.Filters,
	})
	if writerError != nil {
		return nil, writerError
	}
	writer := &statementPdfWriter{pdfWriter: baseWriter, statement: statement}
	return writer.render()
}

func (writer *statementPdfWriter) render() ([]byte, error) {
	headerRows := writer.letterheadRows()
	for _, headerRow := range headerRows {
		writer.headerHeights = append(writer.headerHeights, headerRow.GetHeight(nil, nil))
	}
	headerError := writer.maroto.RegisterHeader(headerRows...)
	if headerError != nil {
		return nil, fmt.Errorf("failed to add the statement letterhead: %w", headerError)
	}
	writer.currentHeight = 0
	for _, headerHeight := range writer.headerHeights {
		writer.currentHeight += headerHeight
	}

	writer.addTitleBlock()
	writer.addColumnHeadings()
	for rowIndex, statementRow := range writer.statement.Rows {
		writer.addStatementRow(rowIndex, statementRow)
	}
	writer.add(row.New(6))
	writer.addStatementNotes()
	writer.addSignatures()

	generatedDocument, generateError := writer.maroto.Generate()
	if generateError != nil {
		return nil, fmt.Errorf("failed to build the statement PDF: %w", generateError)
	}
	return generatedDocument.GetBytes(), nil
}

func (writer *statementPdfWriter) addColumnHeadings() {
	headingStyle := props.Text{Size: 8, Style: fontstyle.Bold, Top: 2.2, Align: align.Right, Right: pdfCellPadding}
	mutedHeadingStyle := headingStyle
	mutedHeadingStyle.Color = pdfMutedText
	headingRow := row.New(7.5).Add(
		text.NewCol(statementLabelColumns, writer.currencyHeading(), props.Text{Size: 7.5, Color: pdfMutedText, Top: 2.4, Left: pdfCellPadding}),
		text.NewCol(statementAmountColumns, writer.statement.CurrentHeading, headingStyle),
		text.NewCol(statementAmountColumns, writer.statement.PreviousHeading, mutedHeadingStyle),
		text.NewCol(statementChangeColumns, Label(writer.language, "statementChange"), mutedHeadingStyle),
	).WithStyle(&props.Cell{BorderType: border.Bottom, BorderColor: writer.brandColor, BorderThickness: 0.5})
	writer.add(headingRow)
	writer.add(row.New(1.5))
}

func (writer *statementPdfWriter) currencyHeading() string {
	if writer.branding.CurrencyCode == "" {
		return ""
	}
	return Label(writer.language, "statementAmountsIn", "{currency}", writer.branding.CurrencyCode)
}

func (writer *statementPdfWriter) addStatementRow(rowIndex int, statementRow StatementRow) {
	rowHeight := statementRowHeight(statementRow.Style)
	if statementRow.Style == StatementHeading {
		writer.keepHeadingWithRows(rowIndex, rowHeight)
	}
	if !writer.fits(rowHeight) {
		writer.addColumnHeadingsOnNewPage()
		if statementRow.Style != StatementHeading && writer.currentSection != "" {
			writer.add(writer.sectionHeading(writer.currentSection + " " + Label(writer.language, "statementContinued")))
		}
	}

	switch statementRow.Style {
	case StatementHeading:
		writer.currentSection = strings.ToUpper(statementRow.Label)
		writer.add(writer.sectionHeading(writer.currentSection))
	case StatementEmpty:
		writer.add(row.New(rowHeight).Add(
			text.NewCol(pdfGridSize, statementRow.Label, props.Text{Size: 8.5, Style: fontstyle.Italic, Color: pdfMutedText, Top: 1.5, Left: pdfCellPadding + statementItemIndent}),
		))
	case StatementRatio:
		writer.add(writer.ratioRow(statementRow, rowHeight))
	default:
		writer.add(writer.amountRow(statementRow, rowHeight))
		if statementRow.Style == StatementGrandTotal {
			writer.add(writer.doubleRule())
		}
	}
}

func (writer *statementPdfWriter) sectionHeading(headingText string) core.Row {
	return row.New(statementRowHeight(StatementHeading)).Add(
		text.NewCol(pdfGridSize, headingText, props.Text{Size: 8.5, Style: fontstyle.Bold, Color: writer.brandColor, Top: 3.6, Left: pdfCellPadding}),
	)
}

func (writer *statementPdfWriter) keepHeadingWithRows(rowIndex int, headingHeight float64) {
	followingHeight := 0.0
	for _, followingRow := range writer.statement.Rows[rowIndex+1 : min(rowIndex+3, len(writer.statement.Rows))] {
		followingHeight += statementRowHeight(followingRow.Style)
	}
	if !writer.fits(headingHeight + followingHeight) {
		writer.addColumnHeadingsOnNewPage()
	}
}

func (writer *statementPdfWriter) addColumnHeadingsOnNewPage() {
	if writer.isAtPageTop() {
		return
	}
	writer.maroto.AddPages(page.New())
	writer.startNewPage()
	writer.addColumnHeadings()
}

func (writer *statementPdfWriter) isAtPageTop() bool {
	headerOnlyHeight := 0.0
	for _, headerHeight := range writer.headerHeights {
		headerOnlyHeight += headerHeight
	}
	return writer.currentHeight == headerOnlyHeight
}

func (writer *statementPdfWriter) amountRow(statementRow StatementRow, rowHeight float64) core.Row {
	fontStyle := fontstyle.Normal
	fontSize := statementFontSize
	labelLeft := pdfCellPadding
	switch statementRow.Style {
	case StatementItem:
		labelLeft += statementItemIndent
	case StatementSubtotal, StatementTotal:
		fontStyle = fontstyle.Bold
	case StatementGrandTotal:
		fontStyle = fontstyle.Bold
		fontSize = 10.5
	}
	textTop := (rowHeight - fontSize*pdfPointToMm) / 2

	labelComponents := []core.Component{}
	labelWidth := writer.columnWidth(statementLabelColumns) - labelLeft - pdfCellPadding
	if statementRow.Code != "" {
		labelComponents = append(labelComponents, text.New(statementRow.Code, props.Text{Size: 7.5, Color: pdfMutedText, Top: textTop + 0.3, Left: labelLeft}))
		labelLeft += statementCodeWidth
		labelWidth -= statementCodeWidth
	}
	labelComponents = append(labelComponents, text.New(truncateToWidth(statementRow.Label, labelWidth, fontSize), props.Text{Size: fontSize, Style: fontStyle, Top: textTop, Left: labelLeft}))

	amountStyle := props.Text{Size: fontSize, Style: fontStyle, Top: textTop, Align: align.Right, Right: pdfCellPadding}
	previousStyle := amountStyle
	previousStyle.Color = pdfMutedText
	changeStyle := props.Text{Size: 8, Color: pdfMutedText, Top: textTop + 0.3, Align: align.Right, Right: pdfCellPadding}

	labelCell, amountCell := writer.cellStyles(statementRow.Style)
	return row.New(rowHeight).Add(
		col.New(statementLabelColumns).Add(labelComponents...).WithStyle(labelCell),
		text.NewCol(statementAmountColumns, StatementAmount(statementRow.Current, writer.branding.CurrencyDecimals), amountStyle).WithStyle(amountCell),
		text.NewCol(statementAmountColumns, StatementAmount(statementRow.Previous, writer.branding.CurrencyDecimals), previousStyle).WithStyle(amountCell),
		text.NewCol(statementChangeColumns, statementRow.Change(), changeStyle).WithStyle(amountCell),
	)
}

func (writer *statementPdfWriter) cellStyles(style StatementStyle) (*props.Cell, *props.Cell) {
	switch style {
	case StatementSubtotal:
		return nil, &props.Cell{BorderType: border.Top, BorderColor: statementRuleColor, BorderThickness: 0.25}
	case StatementTotal:
		return &props.Cell{BackgroundColor: statementTotalFill}, &props.Cell{BackgroundColor: statementTotalFill, BorderType: border.Top, BorderColor: statementRuleColor, BorderThickness: 0.25}
	case StatementGrandTotal:
		return &props.Cell{BackgroundColor: statementTotalFill}, &props.Cell{BackgroundColor: statementTotalFill, BorderType: border.Top | border.Bottom, BorderColor: statementRuleColor, BorderThickness: 0.35}
	default:
		return nil, nil
	}
}

func (writer *statementPdfWriter) doubleRule() core.Row {
	ruleCell := &props.Cell{BorderType: border.Bottom, BorderColor: statementRuleColor, BorderThickness: 0.35}
	return row.New(1).Add(
		col.New(statementLabelColumns),
		col.New(statementAmountColumns).WithStyle(ruleCell),
		col.New(statementAmountColumns).WithStyle(ruleCell),
		col.New(statementChangeColumns).WithStyle(ruleCell),
	)
}

func (writer *statementPdfWriter) ratioRow(statementRow StatementRow, rowHeight float64) core.Row {
	ratioStyle := props.Text{Size: 8, Style: fontstyle.Italic, Color: pdfMutedText, Top: 1.2, Align: align.Right, Right: pdfCellPadding}
	return row.New(rowHeight).Add(
		text.NewCol(statementLabelColumns, statementRow.Label, props.Text{Size: 8, Style: fontstyle.Italic, Color: pdfMutedText, Top: 1.2, Left: pdfCellPadding + statementItemIndent}),
		text.NewCol(statementAmountColumns, writer.ratioText(statementRow, false), ratioStyle),
		text.NewCol(statementAmountColumns, writer.ratioText(statementRow, true), ratioStyle),
		col.New(statementChangeColumns),
	)
}

func (writer *statementPdfWriter) ratioText(statementRow StatementRow, usePrevious bool) string {
	ratioBasisPoints, hasRatio := writer.statement.ratio(statementRow, usePrevious)
	if !hasRatio {
		return "–"
	}
	return strconv.FormatFloat(float64(ratioBasisPoints)/100, 'f', 1, 64) + "%"
}

func (writer *statementPdfWriter) addStatementNotes() {
	if len(writer.statement.Notes) == 0 {
		return
	}
	noteWidth := writer.contentWidth - 2*pdfCellPadding - 5
	noteLines := [][]string{}
	notesHeight := 7.0
	for _, note := range writer.statement.Notes {
		wrappedLines := wrapToWidth(note, noteWidth, statementNoteFontSize)
		noteLines = append(noteLines, wrappedLines)
		notesHeight += float64(len(wrappedLines)) * statementNoteLine
	}
	writer.keepTogether(min(notesHeight, 40))
	writer.add(text.NewRow(7, Label(writer.language, "statementNotes"), props.Text{Size: 8.5, Style: fontstyle.Bold, Top: 1.5, Left: pdfCellPadding}))
	for noteIndex, wrappedLines := range noteLines {
		for lineIndex, noteLine := range wrappedLines {
			lineRow := row.New(statementNoteLine)
			numberText := ""
			if lineIndex == 0 {
				numberText = strconv.Itoa(noteIndex+1) + "."
			}
			lineRow.Add(col.New(pdfGridSize).Add(
				text.New(numberText, props.Text{Size: statementNoteFontSize, Color: pdfMutedText, Top: 0.4, Left: pdfCellPadding}),
				text.New(noteLine, props.Text{Size: statementNoteFontSize, Color: pdfMutedText, Top: 0.4, Left: pdfCellPadding + 5}),
			))
			writer.add(lineRow)
		}
	}
	writer.add(row.New(8))
}

func (writer *statementPdfWriter) addSignatures() {
	signatureHeight := 22.0
	writer.keepTogether(signatureHeight)
	lineStyle := &props.Cell{BorderType: border.Bottom, BorderColor: pdfDividerLine, BorderThickness: 0.3}
	labelStyle := props.Text{Size: 7.5, Color: pdfMutedText, Top: 1.2}
	writer.add(row.New(10).Add(
		text.NewCol(30, nameOrBalce(writer.statement.GeneratedBy), props.Text{Size: 9, Top: 5}).WithStyle(lineStyle),
		col.New(5),
		col.New(30).WithStyle(lineStyle),
		col.New(5),
		col.New(30).WithStyle(lineStyle),
	))
	writer.add(row.New(6).Add(
		text.NewCol(30, Label(writer.language, "statementPreparedBy"), labelStyle),
		col.New(5),
		text.NewCol(30, Label(writer.language, "statementCheckedBy"), labelStyle),
		col.New(5),
		text.NewCol(30, Label(writer.language, "statementDate"), labelStyle),
	))
}

func statementRowHeight(style StatementStyle) float64 {
	switch style {
	case StatementHeading:
		return 9
	case StatementItem, StatementLine, StatementEmpty:
		return 6.2
	case StatementSubtotal:
		return 7
	case StatementTotal:
		return 8
	case StatementGrandTotal:
		return 9.5
	case StatementRatio:
		return 5.2
	default:
		return 6.2
	}
}

func wrapToWidth(value string, widthMillimeters float64, fontSize float64) []string {
	characterWidth := fontSize * pdfPointToMm * pdfAverageCharEm
	capacity := max(int(widthMillimeters/characterWidth), 10)
	wrappedLines := []string{}
	currentLine := ""
	for _, word := range strings.Fields(value) {
		candidateLine := word
		if currentLine != "" {
			candidateLine = currentLine + " " + word
		}
		if len([]rune(candidateLine)) > capacity && currentLine != "" {
			wrappedLines = append(wrappedLines, currentLine)
			currentLine = word
			continue
		}
		currentLine = candidateLine
	}
	if currentLine != "" {
		wrappedLines = append(wrappedLines, currentLine)
	}
	return wrappedLines
}
