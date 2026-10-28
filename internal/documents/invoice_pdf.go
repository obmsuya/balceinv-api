package documents

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/johnfercher/maroto/v2/pkg/components/code"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/image"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/border"
	"github.com/johnfercher/maroto/v2/pkg/consts/extension"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

const (
	invoiceLineHeight    = 4.1
	invoiceRowPadding    = 3.2
	invoiceBodyFontSize  = 8.5
	invoiceSmallFontSize = 7.2
	invoiceBoxPadding    = 3.0
	invoiceTotalsColumns = 42
	invoiceGapColumns    = 4
)

var (
	invoiceToneColors = map[Tone]*props.Color{
		ToneNeutral: {Red: 71, Green: 85, Blue: 105},
		ToneGood:    {Red: 21, Green: 128, Blue: 61},
		ToneWarning: {Red: 180, Green: 83, Blue: 9},
		ToneBad:     {Red: 185, Green: 28, Blue: 28},
	}
	invoiceBoxFill = &props.Color{Red: 247, Green: 248, Blue: 250}
)

type invoicePdfWriter struct {
	*pdfWriter
	invoice InvoiceDocument
}

type invoiceColumn struct {
	title  string
	size   int
	isLeft bool
	value  func(invoiceLine InvoiceLine, position int) string
}

func InvoicePdf(branding Branding, invoice InvoiceDocument) ([]byte, error) {
	baseWriter, writerError := newPdfWriter(branding, Document{
		Language:    invoice.Language,
		Title:       invoice.Title + " " + invoice.Number,
		GeneratedBy: invoice.GeneratedBy,
		GeneratedAt: invoice.GeneratedAt,
	})
	if writerError != nil {
		return nil, writerError
	}
	writer := &invoicePdfWriter{pdfWriter: baseWriter, invoice: invoice}
	return writer.render()
}

func (writer *invoicePdfWriter) render() ([]byte, error) {
	headerRows := writer.invoiceHeaderRows()
	for _, headerRow := range headerRows {
		writer.headerHeights = append(writer.headerHeights, headerRow.GetHeight(nil, nil))
	}
	headerError := writer.maroto.RegisterHeader(headerRows...)
	if headerError != nil {
		return nil, fmt.Errorf("failed to add the invoice letterhead: %w", headerError)
	}
	writer.startNewPage()
	writer.pageCount = 1

	writer.addPartyAndFacts()
	writer.addLines()
	writer.addTotalsBlock()
	writer.addTermsAndNotes()
	writer.addInvoiceQrCode()
	writer.addSignatures()

	generatedDocument, generateError := writer.maroto.Generate()
	if generateError != nil {
		return nil, fmt.Errorf("failed to build the invoice PDF: %w", generateError)
	}
	return generatedDocument.GetBytes(), nil
}

func (writer *invoicePdfWriter) invoiceHeaderRows() []core.Row {
	companyColumns := 60
	companyText := []core.Component{
		text.New(writer.branding.CompanyName, props.Text{Size: 15, Style: fontstyle.Bold, Color: writer.brandColor, Top: 1}),
	}
	nextTop := 8.5
	companyWidth := writer.columnWidth(companyColumns)
	if len(writer.branding.LogoPng) > 0 {
		companyWidth = writer.columnWidth(companyColumns - 18)
	}
	for _, detailLine := range []string{writer.branding.taxLine(writer.language), writer.branding.contactLine(), writer.branding.Email} {
		if strings.TrimSpace(detailLine) == "" {
			continue
		}
		companyText = append(companyText, text.New(truncateToWidth(detailLine, companyWidth, 8), props.Text{Size: 8, Color: pdfMutedText, Top: nextTop}))
		nextTop += 3.9
	}

	headerHeight := max(24.0, nextTop+2)
	letterheadRow := row.New(headerHeight)
	if len(writer.branding.LogoPng) > 0 {
		letterheadRow.Add(
			image.NewFromBytesCol(16, writer.branding.LogoPng, extension.Png, props.Rect{Percent: 90, Center: true}),
			col.New(2),
			col.New(companyColumns-18).Add(companyText...),
		)
	} else {
		letterheadRow.Add(col.New(companyColumns).Add(companyText...))
	}

	titleText := writer.invoice.Title
	titleComponents := []core.Component{
		text.New(titleText, props.Text{Size: titleSizeFor(titleText), Style: fontstyle.Bold, Color: pdfDarkText, Align: align.Right, Top: 0.5}),
	}
	if writer.invoice.Number != "" {
		titleComponents = append(titleComponents, text.New(Label(writer.language, "number")+" "+writer.invoice.Number, props.Text{Size: 10, Style: fontstyle.Bold, Color: writer.brandColor, Align: align.Right, Top: 10}))
	}
	letterheadRow.Add(col.New(pdfGridSize - companyColumns).Add(titleComponents...))

	brandBar := row.New(1.4).Add(col.New(pdfGridSize)).WithStyle(&props.Cell{BackgroundColor: writer.brandColor})
	return []core.Row{letterheadRow, row.New(2), brandBar, row.New(5)}
}

func titleSizeFor(titleText string) float64 {
	if utf8.RuneCountInString(titleText) > 16 {
		return 15
	}
	return 20
}

func (writer *invoicePdfWriter) addPartyAndFacts() {
	if writer.invoice.Status != "" {
		statusColor := invoiceToneColors[writer.invoice.StatusTone]
		chipColumns := min(max(utf8.RuneCountInString(writer.invoice.Status)*2+8, 14), 40)
		writer.add(row.New(7).Add(
			col.New(pdfGridSize-chipColumns),
			text.NewCol(chipColumns, strings.ToUpper(writer.invoice.Status), props.Text{Size: 8.5, Style: fontstyle.Bold, Color: pdfWhite, Align: align.Center, Top: 1.6}).
				WithStyle(&props.Cell{BackgroundColor: statusColor}),
		))
		writer.add(row.New(3))
	}

	boxColumns := (pdfGridSize - invoiceGapColumns) / 2
	boxWidth := writer.columnWidth(boxColumns) - 2*invoiceBoxPadding

	partyComponents := []core.Component{
		text.New(strings.ToUpper(writer.invoice.Party.Heading), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor, Top: invoiceBoxPadding, Left: invoiceBoxPadding}),
	}
	partyTop := invoiceBoxPadding + 4.6
	for _, nameLine := range wrapText(writer.invoice.Party.Name, boxWidth, 10.5, pdfBoldCharEm) {
		partyComponents = append(partyComponents, text.New(nameLine, props.Text{Size: 10.5, Style: fontstyle.Bold, Top: partyTop, Left: invoiceBoxPadding}))
		partyTop += 4.8
	}
	for _, partyLine := range writer.invoice.Party.Lines {
		for _, wrappedLine := range wrapText(partyLine, boxWidth, invoiceBodyFontSize, pdfAverageCharEm) {
			partyComponents = append(partyComponents, text.New(wrappedLine, props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText, Top: partyTop, Left: invoiceBoxPadding}))
			partyTop += invoiceLineHeight
		}
	}

	factComponents := []core.Component{
		text.New(strings.ToUpper(Label(writer.language, "invoiceDetails")), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor, Top: invoiceBoxPadding, Left: invoiceBoxPadding}),
	}
	factTop := invoiceBoxPadding + 4.8
	for _, fact := range writer.invoice.Facts {
		factValue := FormatValue(writer.branding, writer.language, fact.Kind, fact.Value)
		if fact.Kind == Money {
			factValue = FormatCurrency(moneyOf(fact.Value), writer.branding)
		}
		factStyle := fontstyle.Normal
		if fact.Strong {
			factStyle = fontstyle.Bold
		}
		factComponents = append(factComponents,
			text.New(fact.Label, props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText, Top: factTop, Left: invoiceBoxPadding}),
			text.New(truncateToWidth(factValue, boxWidth*0.6, invoiceBodyFontSize), props.Text{Size: invoiceBodyFontSize, Style: factStyle, Top: factTop, Align: align.Right, Right: invoiceBoxPadding}),
		)
		factTop += invoiceLineHeight + 0.6
	}

	boxHeight := max(partyTop, factTop) + invoiceBoxPadding - 0.5
	boxStyle := &props.Cell{BackgroundColor: invoiceBoxFill, BorderType: border.Top, BorderColor: writer.brandColor, BorderThickness: 0.7}
	writer.keepTogether(boxHeight)
	writer.add(row.New(boxHeight).Add(
		col.New(boxColumns).Add(partyComponents...).WithStyle(boxStyle),
		col.New(invoiceGapColumns),
		col.New(boxColumns).Add(factComponents...).WithStyle(boxStyle),
	))
	writer.add(row.New(6))
}

func (writer *invoicePdfWriter) lineColumns() []invoiceColumn {
	quantityText := func(invoiceLine InvoiceLine, position int) string { return FormatInteger(invoiceLine.Quantity) }
	unitText := func(invoiceLine InvoiceLine, position int) string { return invoiceLine.Unit }
	positionText := func(invoiceLine InvoiceLine, position int) string { return strconv.Itoa(position) }
	money := func(value int64) string { return FormatMoney(value, writer.branding.CurrencyDecimals) }

	if writer.invoice.HidePrices {
		return []invoiceColumn{
			{title: "#", size: 5, isLeft: true, value: positionText},
			{title: Label(writer.language, "description"), size: 61, isLeft: true},
			{title: Label(writer.language, "quantity"), size: 10, value: quantityText},
			{title: Label(writer.language, "unit"), size: 10, isLeft: true, value: unitText},
			{title: Label(writer.language, "received"), size: 14, value: func(invoiceLine InvoiceLine, position int) string { return "" }},
		}
	}

	hasDiscount := false
	for _, invoiceLine := range writer.invoice.Lines {
		hasDiscount = hasDiscount || invoiceLine.Discount != 0
	}
	descriptionSize := 44
	if !hasDiscount {
		descriptionSize = 55
	}
	columns := []invoiceColumn{
		{title: "#", size: 5, isLeft: true, value: positionText},
		{title: Label(writer.language, "description"), size: descriptionSize, isLeft: true},
		{title: Label(writer.language, "quantity"), size: 8, value: quantityText},
		{title: Label(writer.language, "unit"), size: 8, isLeft: true, value: unitText},
		{title: Label(writer.language, "unitPrice"), size: 12, value: func(invoiceLine InvoiceLine, position int) string { return money(invoiceLine.UnitPrice) }},
	}
	if hasDiscount {
		columns = append(columns, invoiceColumn{title: Label(writer.language, "discount"), size: 11, value: func(invoiceLine InvoiceLine, position int) string {
			if invoiceLine.Discount == 0 {
				return ""
			}
			return "-" + money(invoiceLine.Discount)
		}})
	}
	columns = append(columns, invoiceColumn{title: Label(writer.language, "amount"), size: 12, value: func(invoiceLine InvoiceLine, position int) string { return money(invoiceLine.Amount) }})
	return columns
}

func (writer *invoicePdfWriter) addLines() {
	columns := writer.lineColumns()
	headerHeight := 7.0
	addHeader := func() {
		headerRow := row.New(headerHeight)
		for _, column := range columns {
			columnAlign := align.Right
			if column.isLeft {
				columnAlign = align.Left
			}
			headerRow.Add(text.NewCol(column.size, strings.ToUpper(column.title), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: pdfWhite, Top: 2.2, Left: pdfCellPadding, Right: pdfCellPadding, Align: columnAlign}))
		}
		writer.add(headerRow.WithStyle(&props.Cell{BackgroundColor: writer.brandColor}))
	}

	writer.keepTogether(headerHeight + 3*(invoiceLineHeight+invoiceRowPadding))
	addHeader()
	if len(writer.invoice.Lines) == 0 {
		writer.add(text.NewRow(8, Label(writer.language, "nothingToShow"), props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText, Top: 2.5, Left: pdfCellPadding}))
	}

	descriptionWidth := 0.0
	for _, column := range columns {
		if column.value == nil {
			descriptionWidth = writer.columnWidth(column.size) - 2*pdfCellPadding
		}
	}
	for lineIndex, invoiceLine := range writer.invoice.Lines {
		descriptionLines := wrapText(invoiceLine.Description, descriptionWidth, invoiceBodyFontSize, pdfAverageCharEm)
		detailLines := wrapText(invoiceLine.Detail, descriptionWidth, invoiceSmallFontSize, pdfAverageCharEm)
		rowHeight := float64(len(descriptionLines))*invoiceLineHeight + float64(len(detailLines))*3.4 + invoiceRowPadding
		if !writer.fits(rowHeight) {
			writer.keepTogether(rowHeight)
			addHeader()
		}

		lineRow := row.New(rowHeight)
		for _, column := range columns {
			if column.value == nil {
				descriptionComponents := []core.Component{}
				lineTop := invoiceRowPadding / 2
				for _, descriptionLine := range descriptionLines {
					descriptionComponents = append(descriptionComponents, text.New(descriptionLine, props.Text{Size: invoiceBodyFontSize, Top: lineTop, Left: pdfCellPadding}))
					lineTop += invoiceLineHeight
				}
				for _, detailLine := range detailLines {
					descriptionComponents = append(descriptionComponents, text.New(detailLine, props.Text{Size: invoiceSmallFontSize, Color: pdfMutedText, Top: lineTop, Left: pdfCellPadding}))
					lineTop += 3.4
				}
				lineRow.Add(col.New(column.size).Add(descriptionComponents...))
				continue
			}
			columnAlign := align.Right
			if column.isLeft {
				columnAlign = align.Left
			}
			cellText := truncateToWidth(column.value(invoiceLine, lineIndex+1), writer.columnWidth(column.size)-2*pdfCellPadding, invoiceBodyFontSize)
			lineRow.Add(text.NewCol(column.size, cellText, props.Text{Size: invoiceBodyFontSize, Top: invoiceRowPadding / 2, Left: pdfCellPadding, Right: pdfCellPadding, Align: columnAlign, Color: mutedWhen(column.title == "#")}))
		}
		writer.add(lineRow.WithStyle(&props.Cell{BorderType: border.Bottom, BorderColor: pdfDividerLine, BorderThickness: 0.2}))
	}
	writer.add(row.New(5))
}

func mutedWhen(isMuted bool) *props.Color {
	if isMuted {
		return pdfMutedText
	}
	return pdfDarkText
}

func (writer *invoicePdfWriter) addTotalsBlock() {
	totals := writer.invoice.Totals
	leftColumns := pdfGridSize - invoiceTotalsColumns - invoiceGapColumns
	leftWidth := writer.columnWidth(leftColumns) - 2*invoiceBoxPadding

	leftComponents := []core.Component{}
	leftTop := 0.0
	addLeftSection := func(heading string, body string, bodyStyle fontstyle.Type) {
		if strings.TrimSpace(body) == "" {
			return
		}
		leftComponents = append(leftComponents, text.New(strings.ToUpper(heading), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor, Top: leftTop}))
		leftTop += 4.4
		for _, paragraph := range strings.Split(body, "\n") {
			for _, bodyLine := range wrapText(paragraph, leftWidth, invoiceBodyFontSize, pdfAverageCharEm) {
				leftComponents = append(leftComponents, text.New(bodyLine, props.Text{Size: invoiceBodyFontSize, Style: bodyStyle, Top: leftTop}))
				leftTop += invoiceLineHeight
			}
		}
		leftTop += 3
	}
	addLeftSection(Label(writer.language, "amountInWords"), writer.invoice.AmountInWords, fontstyle.Italic)
	addLeftSection(Label(writer.language, "paymentDetails"), writer.invoice.PaymentDetails, fontstyle.Normal)

	highlightIndex := -1
	for totalIndex, total := range totals {
		if total.Strong {
			highlightIndex = totalIndex
		}
	}

	rightComponents := []core.Component{}
	rightTop := 0.5
	for totalIndex, total := range totals {
		if totalIndex == highlightIndex {
			continue
		}
		totalValue := writer.formatTotal(total)
		totalStyle := fontstyle.Normal
		totalSize := invoiceBodyFontSize + 0.5
		if total.Strong {
			totalStyle = fontstyle.Bold
		}
		rightComponents = append(rightComponents,
			text.New(total.Label, props.Text{Size: totalSize, Style: totalStyle, Color: mutedWhen(!total.Strong), Top: rightTop, Left: invoiceBoxPadding}),
			text.New(totalValue, props.Text{Size: totalSize, Style: totalStyle, Top: rightTop, Align: align.Right, Right: invoiceBoxPadding}),
		)
		rightTop += 5.6
	}

	blockHeight := max(leftTop, rightTop)
	highlightHeight := 0.0
	if highlightIndex >= 0 {
		highlightHeight = 10.0
	}
	writer.keepTogether(blockHeight + highlightHeight + 2)

	if highlightIndex < 0 {
		writer.add(row.New(blockHeight).Add(
			col.New(leftColumns).Add(leftComponents...),
			col.New(invoiceGapColumns),
			col.New(invoiceTotalsColumns).Add(rightComponents...),
		))
		writer.add(row.New(5))
		return
	}

	highlight := totals[highlightIndex]
	leftHeightOnFirstRow := max(rightTop, 0.1)
	writer.add(row.New(leftHeightOnFirstRow).Add(
		col.New(leftColumns).Add(leftComponents...),
		col.New(invoiceGapColumns),
		col.New(invoiceTotalsColumns).Add(rightComponents...),
	))
	writer.add(row.New(highlightHeight).Add(
		col.New(leftColumns),
		col.New(invoiceGapColumns),
		col.New(invoiceTotalsColumns).Add(
			text.New(highlight.Label, props.Text{Size: 9.5, Style: fontstyle.Bold, Color: pdfWhite, Top: 3, Left: invoiceBoxPadding}),
			text.New(writer.formatTotal(highlight), props.Text{Size: 12, Style: fontstyle.Bold, Color: pdfWhite, Top: 2.4, Align: align.Right, Right: invoiceBoxPadding}),
		).WithStyle(&props.Cell{BackgroundColor: writer.brandColor}),
	))
	remainingLeftHeight := leftTop - leftHeightOnFirstRow - highlightHeight
	if remainingLeftHeight > 0 {
		writer.add(row.New(remainingLeftHeight))
	}
	writer.add(row.New(6))
}

func (writer *invoicePdfWriter) formatTotal(total Field) string {
	if total.Kind == Money {
		return FormatCurrency(moneyOf(total.Value), writer.branding)
	}
	return FormatValue(writer.branding, writer.language, total.Kind, total.Value)
}

func (writer *invoicePdfWriter) addTermsAndNotes() {
	sections := []struct {
		heading string
		body    string
	}{
		{Label(writer.language, "terms"), writer.invoice.Terms},
		{Label(writer.language, "statementNotes"), strings.Join(writer.invoice.Notes, "\n")},
	}
	for _, section := range sections {
		if strings.TrimSpace(section.body) == "" {
			continue
		}
		bodyLines := []string{}
		for _, paragraph := range strings.Split(section.body, "\n") {
			bodyLines = append(bodyLines, wrapText(paragraph, writer.contentWidth, invoiceBodyFontSize, pdfAverageCharEm)...)
		}
		writer.keepTogether(5 + min(float64(len(bodyLines)), 4)*invoiceLineHeight)
		writer.add(text.NewRow(5, strings.ToUpper(section.heading), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor}))
		for _, bodyLine := range bodyLines {
			writer.add(text.NewRow(invoiceLineHeight, bodyLine, props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText}))
		}
		writer.add(row.New(4))
	}
}

func (writer *invoicePdfWriter) addInvoiceQrCode() {
	if writer.invoice.QrCode == "" && writer.invoice.QrCaption == "" {
		return
	}
	captionComponents := []core.Component{}
	captionTop := 5.0
	for _, captionLine := range strings.Split(writer.invoice.QrCaption, "\n") {
		if strings.TrimSpace(captionLine) == "" {
			continue
		}
		captionComponents = append(captionComponents, text.New(captionLine, props.Text{Size: invoiceBodyFontSize, Top: captionTop}))
		captionTop += 4.5
	}
	qrRow := row.New(26)
	if writer.invoice.QrCode != "" {
		qrRow.Add(code.NewQrCol(14, writer.invoice.QrCode, props.Rect{Percent: 100}), col.New(3))
	}
	qrRow.Add(col.New(60).Add(captionComponents...))
	writer.keepTogether(26)
	writer.add(qrRow)
	writer.add(row.New(4))
}

func (writer *invoicePdfWriter) addSignatures() {
	signatureLabels := writer.invoice.Signatures
	if len(signatureLabels) == 0 {
		return
	}
	signatureCount := min(len(signatureLabels), 3)
	gapColumns := 6
	signatureColumns := (pdfGridSize - gapColumns*(signatureCount-1)) / signatureCount
	writer.keepTogether(24)
	writer.add(row.New(8))

	lineStyle := &props.Cell{BorderType: border.Bottom, BorderColor: pdfMutedText, BorderThickness: 0.3}
	lineRow := row.New(10)
	labelRow := row.New(6)
	for signatureIndex, signatureLabel := range signatureLabels[:signatureCount] {
		if signatureIndex > 0 {
			lineRow.Add(col.New(gapColumns))
			labelRow.Add(col.New(gapColumns))
		}
		signedName := ""
		if signatureIndex == 0 {
			signedName = nameOrBalce(writer.invoice.GeneratedBy)
		}
		lineRow.Add(text.NewCol(signatureColumns, signedName, props.Text{Size: 9, Top: 5}).WithStyle(lineStyle))
		labelRow.Add(text.NewCol(signatureColumns, signatureLabel, props.Text{Size: invoiceSmallFontSize, Color: pdfMutedText, Top: 1.2}))
	}
	writer.add(lineRow)
	writer.add(labelRow)
}

func wrapText(value string, widthMillimeters float64, fontSize float64, charEm float64) []string {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil
	}
	capacity := max(int(widthMillimeters/(fontSize*pdfPointToMm*charEm)), 4)
	wrappedLines := []string{}
	currentLine := ""
	for _, word := range strings.Fields(trimmedValue) {
		for utf8.RuneCountInString(word) > capacity {
			if currentLine != "" {
				wrappedLines = append(wrappedLines, currentLine)
				currentLine = ""
			}
			wordRunes := []rune(word)
			wrappedLines = append(wrappedLines, string(wordRunes[:capacity]))
			word = string(wordRunes[capacity:])
		}
		candidateLine := word
		if currentLine != "" {
			candidateLine = currentLine + " " + word
		}
		if utf8.RuneCountInString(candidateLine) > capacity {
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
