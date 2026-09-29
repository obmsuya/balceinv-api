package documents

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/code"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/image"
	"github.com/johnfercher/maroto/v2/pkg/components/page"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/border"
	"github.com/johnfercher/maroto/v2/pkg/consts/extension"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/consts/orientation"
	"github.com/johnfercher/maroto/v2/pkg/consts/pagesize"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/fontrepository"
	"github.com/johnfercher/maroto/v2/pkg/props"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	pdfFontFamily      = "balce-go"
	pdfGridSize        = 100
	pdfMarginSide      = 12.0
	pdfMarginTop       = 10.0
	pdfMarginBottom    = 14.0
	pdfTableRowHeight  = 6.5
	pdfTallHeaderRow   = 9.5
	pdfTableFontSize   = 8.0
	pdfHeaderFontSize  = 7.5
	pdfBoldCharEm      = 0.56
	pdfTextTop         = 1.8
	pdfCellPadding     = 1.5
	pdfCardsPerRow     = 4
	pdfCardHeight      = 15.0
	pdfPointToMm       = 0.3528
	pdfAverageCharEm   = 0.52
	pdfSectionTitleRow = 8.0
)

var (
	pdfDarkText    = &props.Color{Red: 28, Green: 33, Blue: 40}
	pdfMutedText   = &props.Color{Red: 91, Green: 101, Blue: 115}
	pdfWhite       = &props.Color{Red: 255, Green: 255, Blue: 255}
	pdfZebraFill   = &props.Color{Red: 243, Green: 246, Blue: 249}
	pdfTotalsFill  = &props.Color{Red: 231, Green: 236, Blue: 242}
	pdfCardFill    = &props.Color{Red: 245, Green: 247, Blue: 250}
	pdfCardBorder  = &props.Color{Red: 213, Green: 219, Blue: 227}
	pdfDividerLine = &props.Color{Red: 213, Green: 219, Blue: 227}
)

type pdfWriter struct {
	maroto           core.Maroto
	branding         Branding
	document         Document
	language         string
	brandColor       *props.Color
	contentWidth     float64
	contentHeight    float64
	headerHeights    []float64
	currentHeight    float64
	pageCount        int
	tableHeaderPages []int
}

func Pdf(branding Branding, document Document) ([]byte, error) {
	writer, writerError := newPdfWriter(branding, document)
	if writerError != nil {
		return nil, writerError
	}
	return writer.render()
}

func newPdfWriter(branding Branding, document Document) (*pdfWriter, error) {
	language := document.language()
	customFonts, fontsError := fontrepository.New().
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Normal, goregular.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Bold, gobold.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.Italic, goitalic.TTF).
		AddUTF8FontFromBytes(pdfFontFamily, fontstyle.BoldItalic, gobolditalic.TTF).
		Load()
	if fontsError != nil {
		return nil, fmt.Errorf("failed to load the document fonts: %w", fontsError)
	}

	generatedAt := document.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	footerPattern := Label(language, "pdfFooter", "{date}", FormatDateTime(language, generatedAt.In(branding.Location())), "{user}", nameOrBalce(document.GeneratedBy))

	pageOrientation := orientation.Vertical
	if document.Landscape {
		pageOrientation = orientation.Horizontal
	}
	documentConfig := config.NewBuilder().
		WithPageSize(pagesize.A4).
		WithOrientation(pageOrientation).
		WithLeftMargin(pdfMarginSide).
		WithRightMargin(pdfMarginSide).
		WithTopMargin(pdfMarginTop).
		WithBottomMargin(pdfMarginBottom).
		WithMaxGridSize(pdfGridSize).
		WithCustomFonts(customFonts).
		WithDefaultFont(&props.Font{Family: pdfFontFamily, Size: 9, Color: pdfDarkText}).
		WithPageNumber(props.PageNumber{Pattern: footerPattern, Place: props.Bottom, Family: pdfFontFamily, Size: 7.5, Color: pdfMutedText}).
		WithTitle(document.Title, true).
		WithSubject(document.Subtitle, true).
		WithAuthor(nameOrBalce(document.GeneratedBy), true).
		WithCreator("Balce", true).
		WithCreationDate(generatedAt).
		WithCompression(true).
		Build()

	red, green, blue := branding.colorRgb()
	writer := &pdfWriter{
		maroto:        maroto.New(documentConfig),
		branding:      branding,
		document:      document,
		language:      language,
		brandColor:    &props.Color{Red: red, Green: green, Blue: blue},
		contentWidth:  documentConfig.Dimensions.Width - 2*pdfMarginSide,
		contentHeight: documentConfig.Dimensions.Height - pdfMarginTop - pdfMarginBottom,
		pageCount:     1,
	}
	return writer, nil
}

func (writer *pdfWriter) render() ([]byte, error) {
	headerRows := writer.letterheadRows()
	for _, headerRow := range headerRows {
		writer.headerHeights = append(writer.headerHeights, headerRow.GetHeight(nil, nil))
	}
	headerError := writer.maroto.RegisterHeader(headerRows...)
	if headerError != nil {
		return nil, fmt.Errorf("failed to add the document letterhead: %w", headerError)
	}
	writer.currentHeight = 0
	for _, headerHeight := range writer.headerHeights {
		writer.currentHeight += headerHeight
	}

	writer.addTitleBlock()
	writer.addCards()
	for _, table := range writer.document.Tables {
		writer.addTable(table)
	}
	writer.addTotals()
	writer.addQrCode()
	writer.addNotes()

	generatedDocument, generateError := writer.maroto.Generate()
	if generateError != nil {
		return nil, fmt.Errorf("failed to build the PDF: %w", generateError)
	}
	return generatedDocument.GetBytes(), nil
}

func (writer *pdfWriter) add(newRow core.Row) {
	rowHeight := newRow.GetHeight(nil, nil)
	if rowHeight+writer.currentHeight <= writer.contentHeight {
		writer.currentHeight += rowHeight
	} else {
		writer.startNewPage()
		writer.currentHeight += rowHeight
	}
	writer.maroto.AddRows(newRow)
}

func (writer *pdfWriter) startNewPage() {
	writer.pageCount++
	writer.currentHeight = 0
	for _, headerHeight := range writer.headerHeights {
		writer.currentHeight += headerHeight
	}
}

func (writer *pdfWriter) fits(height float64) bool {
	return height+writer.currentHeight <= writer.contentHeight
}

func (writer *pdfWriter) keepTogether(height float64) {
	if writer.fits(height) {
		return
	}
	headerOnlyHeight := 0.0
	for _, headerHeight := range writer.headerHeights {
		headerOnlyHeight += headerHeight
	}
	if writer.currentHeight == headerOnlyHeight {
		return
	}
	writer.maroto.AddPages(page.New())
	writer.startNewPage()
}

func (writer *pdfWriter) letterheadRows() []core.Row {
	companyText := []core.Component{
		text.New(writer.branding.CompanyName, props.Text{Size: 14, Style: fontstyle.Bold, Color: writer.brandColor, Top: 0.5}),
	}
	nextTop := 7.5
	for _, detailLine := range []string{writer.branding.taxLine(writer.language), writer.branding.contactLine()} {
		if detailLine == "" {
			continue
		}
		companyText = append(companyText, text.New(truncateToWidth(detailLine, writer.contentWidth*0.8, 8), props.Text{Size: 8, Color: pdfMutedText, Top: nextTop}))
		nextTop += 4
	}

	letterheadRow := row.New(17)
	if len(writer.branding.LogoPng) > 0 {
		letterheadRow.Add(
			image.NewFromBytesCol(18, writer.branding.LogoPng, extension.Png, props.Rect{Percent: 92, Center: true}),
			col.New(2),
			col.New(80).Add(companyText...),
		)
	} else {
		letterheadRow.Add(col.New(pdfGridSize).Add(companyText...))
	}

	brandBar := row.New(1.2).Add(col.New(pdfGridSize)).WithStyle(&props.Cell{BackgroundColor: writer.brandColor})
	return []core.Row{letterheadRow, row.New(1.5), brandBar, row.New(4)}
}

func (writer *pdfWriter) addTitleBlock() {
	titleRow := row.New(10)
	if writer.document.Number != "" {
		titleRow.Add(
			text.NewCol(65, writer.document.Title, props.Text{Size: 15, Style: fontstyle.Bold, Top: 1}),
			text.NewCol(35, Label(writer.language, "number")+" "+writer.document.Number, props.Text{Size: 10, Style: fontstyle.Bold, Align: align.Right, Top: 3}),
		)
	} else {
		titleRow.Add(text.NewCol(pdfGridSize, writer.document.Title, props.Text{Size: 15, Style: fontstyle.Bold, Top: 1}))
	}
	writer.add(titleRow)

	if writer.document.Subtitle != "" {
		writer.add(text.NewRow(5.5, writer.document.Subtitle, props.Text{Size: 9.5, Color: pdfMutedText, Top: 0.5}))
	}
	if len(writer.document.Filters) > 0 {
		filterParts := []string{}
		for _, filter := range writer.document.Filters {
			filterParts = append(filterParts, filter.Label+": "+FormatValue(writer.branding, writer.language, filter.Kind, filter.Value))
		}
		writer.add(text.NewRow(5, truncateToWidth(strings.Join(filterParts, "  ·  "), writer.contentWidth, 8), props.Text{Size: 8, Color: pdfMutedText, Top: 0.5}))
	}

	for detailStart := 0; detailStart < len(writer.document.Details); detailStart += 2 {
		detailRow := row.New(5.5)
		for _, detail := range writer.document.Details[detailStart:min(detailStart+2, len(writer.document.Details))] {
			detailText := detail.Label + ": " + FormatValue(writer.branding, writer.language, detail.Kind, detail.Value)
			detailRow.Add(text.NewCol(50, truncateToWidth(detailText, writer.contentWidth/2, 9), props.Text{Size: 9, Top: 0.8}))
		}
		writer.add(detailRow)
	}
	writer.add(row.New(4))
}

func (writer *pdfWriter) addCards() {
	if len(writer.document.Cards) == 0 {
		return
	}
	cardWidth := (pdfGridSize - (pdfCardsPerRow - 1)) / pdfCardsPerRow
	for cardStart := 0; cardStart < len(writer.document.Cards); cardStart += pdfCardsPerRow {
		cardRow := row.New(pdfCardHeight)
		for cardIndex, card := range writer.document.Cards[cardStart:min(cardStart+pdfCardsPerRow, len(writer.document.Cards))] {
			if cardIndex > 0 {
				cardRow.Add(col.New(1))
			}
			cardValue := FormatValue(writer.branding, writer.language, card.Kind, card.Value)
			if card.Kind == Money {
				cardValue = FormatCurrency(moneyOf(card.Value), writer.branding)
			}
			cardColumnWidth := writer.contentWidth * float64(cardWidth) / pdfGridSize
			cardRow.Add(col.New(cardWidth).Add(
				text.New(truncateToWidth(card.Label, cardColumnWidth-4, 7.5), props.Text{Size: 7.5, Color: pdfMutedText, Top: 2.2, Left: 2.5}),
				text.New(truncateToWidth(cardValue, cardColumnWidth-4, 11), props.Text{Size: 11, Style: fontstyle.Bold, Top: 7, Left: 2.5}),
			).WithStyle(&props.Cell{BackgroundColor: pdfCardFill, BorderType: border.Full, BorderColor: pdfCardBorder, BorderThickness: 0.2}))
		}
		writer.add(cardRow)
		writer.add(row.New(2.5))
	}
	writer.add(row.New(3))
}

func (writer *pdfWriter) addTable(table Table) {
	columnSizes := columnGridSizes(table.Columns)
	headerHeight := writer.tableHeaderHeight(table.Columns, columnSizes)
	writer.keepTogether(pdfSectionTitleRow + headerHeight + 2*pdfTableRowHeight)
	if table.Title != "" {
		writer.add(text.NewRow(pdfSectionTitleRow, table.Title, props.Text{Size: 11, Style: fontstyle.Bold, Top: 2}))
	}
	writer.addTableHeader(table.Columns, columnSizes, headerHeight)

	if len(table.Rows) == 0 {
		writer.add(text.NewRow(pdfTableRowHeight, Label(writer.language, "nothingToShow"), props.Text{Size: pdfTableFontSize, Color: pdfMutedText, Top: pdfTextTop, Left: pdfCellPadding}))
		writer.add(row.New(5))
		return
	}

	for rowIndex, rowValues := range table.Rows {
		if !writer.fits(pdfTableRowHeight) {
			writer.addTableHeader(table.Columns, columnSizes, headerHeight)
		}
		dataRow := row.New(pdfTableRowHeight)
		for columnIndex, column := range table.Columns {
			var cellValue any
			if columnIndex < len(rowValues) {
				cellValue = rowValues[columnIndex]
			}
			dataRow.Add(writer.tableCell(column.Kind, cellValue, columnSizes[columnIndex], fontstyle.Normal))
		}
		if rowIndex%2 == 1 {
			dataRow.WithStyle(&props.Cell{BackgroundColor: pdfZebraFill})
		}
		writer.add(dataRow)
	}

	totalsRow := writer.totalsRow(table, columnSizes)
	if totalsRow != nil {
		if !writer.fits(pdfTableRowHeight) {
			writer.addTableHeader(table.Columns, columnSizes, headerHeight)
		}
		writer.add(totalsRow)
	}
	writer.add(row.New(5))
}

func (writer *pdfWriter) tableHeaderHeight(columns []Column, columnSizes []int) float64 {
	for columnIndex, column := range columns {
		if utf8.RuneCountInString(column.Title) > writer.headerCapacity(columnSizes[columnIndex]) {
			return pdfTallHeaderRow
		}
	}
	return pdfTableRowHeight
}

func (writer *pdfWriter) headerCapacity(columnSize int) int {
	return int((writer.columnWidth(columnSize) - 2*pdfCellPadding) / (pdfHeaderFontSize * pdfPointToMm * pdfBoldCharEm))
}

func (writer *pdfWriter) addTableHeader(columns []Column, columnSizes []int, headerHeight float64) {
	headerRow := row.New(headerHeight)
	for columnIndex, column := range columns {
		headerTitle := column.Title
		headerCapacity := writer.headerCapacity(columnSizes[columnIndex])
		headerTextTop := pdfTextTop + (headerHeight-pdfTableRowHeight)/2
		if utf8.RuneCountInString(headerTitle) > headerCapacity {
			headerTextTop = 1.5
		}
		if headerHeight == pdfTableRowHeight {
			headerTitle = truncateRunes(headerTitle, max(headerCapacity, 1))
		} else if utf8.RuneCountInString(headerTitle) > 2*headerCapacity-2 {
			headerTitle = truncateRunes(headerTitle, max(2*headerCapacity-3, 1)) + "…"
		}
		headerRow.Add(text.NewCol(columnSizes[columnIndex], headerTitle, props.Text{
			Size:  pdfHeaderFontSize,
			Style: fontstyle.Bold,
			Color: pdfWhite,
			Top:   headerTextTop,
			Left:  pdfCellPadding,
			Right: pdfCellPadding,
			Align: alignmentFor(column.Kind),
		}))
	}
	headerRow.WithStyle(&props.Cell{BackgroundColor: writer.brandColor})
	writer.add(headerRow)
	writer.tableHeaderPages = append(writer.tableHeaderPages, writer.pageCount)
}

func (writer *pdfWriter) totalsRow(table Table, columnSizes []int) core.Row {
	hasSums := false
	for _, column := range table.Columns {
		hasSums = hasSums || column.Sum
	}
	if !hasSums {
		return nil
	}

	totalsRow := row.New(pdfTableRowHeight)
	for columnIndex, column := range table.Columns {
		if !column.Sum {
			cellText := ""
			if columnIndex == 0 {
				cellText = Label(writer.language, "total")
			}
			totalsRow.Add(writer.tableCell(Text, cellText, columnSizes[columnIndex], fontstyle.Bold))
			continue
		}
		columnTotal := int64(0)
		for _, rowValues := range table.Rows {
			if columnIndex < len(rowValues) {
				cellInteger, isInteger := asInteger(rowValues[columnIndex])
				if isInteger {
					columnTotal += cellInteger
				}
			}
		}
		totalsRow.Add(writer.tableCell(column.Kind, columnTotal, columnSizes[columnIndex], fontstyle.Bold))
	}
	return totalsRow.WithStyle(&props.Cell{BackgroundColor: pdfTotalsFill, BorderType: border.Top, BorderColor: writer.brandColor, BorderThickness: 0.4})
}

func (writer *pdfWriter) tableCell(kind Kind, cellValue any, columnSize int, style fontstyle.Type) core.Col {
	cellText := FormatValue(writer.branding, writer.language, kind, cellValue)
	return text.NewCol(columnSize, truncateToWidth(cellText, writer.columnWidth(columnSize)-2*pdfCellPadding, pdfTableFontSize), props.Text{
		Size:  pdfTableFontSize,
		Style: style,
		Top:   pdfTextTop,
		Left:  pdfCellPadding,
		Right: pdfCellPadding,
		Align: alignmentFor(kind),
	})
}

func (writer *pdfWriter) addTotals() {
	if len(writer.document.Totals) == 0 {
		return
	}
	for _, field := range writer.document.Totals {
		fieldValue := FormatValue(writer.branding, writer.language, field.Kind, field.Value)
		if field.Kind == Money {
			fieldValue = FormatCurrency(moneyOf(field.Value), writer.branding)
		}
		fieldSize := 9.0
		fieldHeight := 6.0
		fieldStyle := fontstyle.Normal
		if field.Strong {
			fieldSize = 11.5
			fieldHeight = 8.0
			fieldStyle = fontstyle.Bold
		}
		fieldRow := row.New(fieldHeight).Add(
			col.New(45),
			text.NewCol(30, field.Label, props.Text{Size: fieldSize, Style: fieldStyle, Top: 1.2, Align: align.Right, Right: pdfCellPadding}),
			text.NewCol(25, fieldValue, props.Text{Size: fieldSize, Style: fieldStyle, Top: 1.2, Align: align.Right, Right: pdfCellPadding}),
		)
		if field.Strong {
			fieldRow.WithStyle(&props.Cell{BorderType: border.Top | border.Bottom, BorderColor: pdfDividerLine, BorderThickness: 0.3})
		}
		writer.add(fieldRow)
	}
	writer.add(row.New(5))
}

func (writer *pdfWriter) addQrCode() {
	if writer.document.QrCode == "" && writer.document.QrCaption == "" {
		return
	}
	captionComponents := []core.Component{}
	captionTop := 6.0
	for _, captionLine := range strings.Split(writer.document.QrCaption, "\n") {
		if strings.TrimSpace(captionLine) == "" {
			continue
		}
		captionComponents = append(captionComponents, text.New(captionLine, props.Text{Size: 9, Top: captionTop}))
		captionTop += 5
	}
	qrRow := row.New(30)
	if writer.document.QrCode != "" {
		qrRow.Add(code.NewQrCol(16, writer.document.QrCode, props.Rect{Percent: 100}), col.New(3))
	}
	qrRow.Add(col.New(60).Add(captionComponents...))
	writer.keepTogether(30)
	writer.add(qrRow)
	writer.add(row.New(4))
}

func (writer *pdfWriter) addNotes() {
	for _, note := range writer.document.Notes {
		for _, noteLine := range strings.Split(note, "\n") {
			trimmedLine := strings.TrimSpace(noteLine)
			if trimmedLine == "" {
				continue
			}
			writer.add(text.NewRow(5, truncateToWidth(trimmedLine, writer.contentWidth, 8.5), props.Text{Size: 8.5, Color: pdfMutedText, Align: align.Center, Top: 0.5}))
		}
	}
}

func (writer *pdfWriter) columnWidth(columnSize int) float64 {
	return writer.contentWidth * float64(columnSize) / pdfGridSize
}

func columnGridSizes(columns []Column) []int {
	if len(columns) == 0 {
		return nil
	}
	columnWeights := make([]float64, len(columns))
	totalWeight := 0.0
	isFirstText := true
	for columnIndex, column := range columns {
		switch column.Kind {
		case Text:
			columnWeights[columnIndex] = 2
			if isFirstText {
				columnWeights[columnIndex] = 3.4
				isFirstText = false
			}
		case DateTime:
			columnWeights[columnIndex] = 2.3
		case Date:
			columnWeights[columnIndex] = 1.8
		default:
			columnWeights[columnIndex] = 1.6
		}
		totalWeight += columnWeights[columnIndex]
	}

	columnSizes := make([]int, len(columns))
	usedSize := 0
	for columnIndex := range columns {
		columnSizes[columnIndex] = max(int(columnWeights[columnIndex]/totalWeight*pdfGridSize), 1)
		usedSize += columnSizes[columnIndex]
	}
	columnSizes[0] += pdfGridSize - usedSize
	return columnSizes
}

func alignmentFor(kind Kind) align.Type {
	if kind == Money || kind == Integer || kind == Percent {
		return align.Right
	}
	return align.Left
}

func truncateToWidth(value string, widthMillimeters float64, fontSize float64) string {
	characterWidth := fontSize * pdfPointToMm * pdfAverageCharEm
	capacity := int(widthMillimeters / characterWidth)
	if capacity < 2 || utf8.RuneCountInString(value) <= capacity {
		return value
	}
	return string([]rune(value)[:capacity-1]) + "…"
}

func moneyOf(value any) int64 {
	moneyValue, _ := asInteger(value)
	return moneyValue
}
