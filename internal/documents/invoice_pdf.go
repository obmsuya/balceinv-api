package documents

import (
	"fmt"
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
	invoiceLineHeight    = 4.0
	invoiceRowPadding    = 2.8
	invoiceBodyFontSize  = 8.5
	invoiceSmallFontSize = 7.4
	invoiceCellPadding   = 1.8
	invoiceFactRowHeight = 5.8
	invoiceTotalRow      = 6.2
	invoiceStrongRow     = 7.6
	invoiceLogoColumns   = 18
	invoicePartyColumns  = 54
	invoiceGapColumns    = 4
	invoiceTotalsColumns = 44
	invoiceTotalsLabel   = 25
	invoiceStampColumns  = 24
	invoiceFactLabel     = 17
	invoiceGridThickness = 0.25
)

var (
	invoiceToneColors = map[Tone]*props.Color{
		ToneNeutral: {Red: 71, Green: 85, Blue: 105},
		ToneGood:    {Red: 21, Green: 128, Blue: 61},
		ToneWarning: {Red: 180, Green: 83, Blue: 9},
		ToneBad:     {Red: 185, Green: 28, Blue: 28},
	}
	invoiceGridLine = &props.Color{Red: 120, Green: 128, Blue: 138}
	invoiceHeadFill = &props.Color{Red: 236, Green: 239, Blue: 243}
)

type invoicePdfWriter struct {
	*pdfWriter
	invoice InvoiceDocument
}

type invoiceLeftLine struct {
	text   string
	style  props.Text
	height float64
}

type invoiceColumn struct {
	title     string
	size      int
	alignment align.Type
	value     func(invoiceLine InvoiceLine) string
}

func InvoicePdf(branding Branding, invoice InvoiceDocument) ([]byte, error) {
	baseWriter, writerError := newPdfWriter(branding, Document{
		Language:    invoice.Language,
		Title:       invoice.Title + " " + invoice.Number,
		Subtitle:    invoice.Party.Name,
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

	writer.addTitle()
	writer.addPartyAndFacts()
	writer.addIssuedBy()
	writer.addLines()
	writer.addTotalsBlock()
	writer.addTermsAndPayment()
	writer.addInvoiceQrCode()
	writer.addSignatures()

	generatedDocument, generateError := writer.maroto.Generate()
	if generateError != nil {
		return nil, fmt.Errorf("failed to build the invoice PDF: %w", generateError)
	}
	return generatedDocument.GetBytes(), nil
}

func (writer *invoicePdfWriter) hides(part string) bool {
	return writer.branding.HidesInvoicePart(part)
}

func (writer *invoicePdfWriter) gridCell(borderType border.Type) *props.Cell {
	return &props.Cell{BorderType: borderType, BorderColor: invoiceGridLine, BorderThickness: invoiceGridThickness}
}

func (writer *invoicePdfWriter) invoiceHeaderRows() []core.Row {
	showLogo := len(writer.branding.LogoPng) > 0 && !writer.hides(InvoicePartLogo)
	textColumns := pdfGridSize
	if showLogo {
		textColumns = pdfGridSize - 2*invoiceLogoColumns
	}
	textWidth := writer.columnWidth(textColumns)

	companyText := []core.Component{}
	nextTop := 0.5
	for _, nameLine := range wrapText(writer.branding.CompanyName, textWidth, 17, pdfBoldCharEm) {
		companyText = append(companyText, text.New(nameLine, props.Text{Size: 17, Style: fontstyle.Bold, Color: writer.brandColor, Align: align.Center, Top: nextTop}))
		nextTop += 7.2
	}
	nextTop += 0.8
	detailLines := wrapText(writer.branding.Address, textWidth, 8.5, pdfAverageCharEm)
	if phone := strings.TrimSpace(writer.branding.Phone); phone != "" {
		detailLines = append(detailLines, Label(writer.language, "phone")+": "+strings.Join(strings.Fields(phone), " "))
	}
	if email := strings.TrimSpace(writer.branding.Email); email != "" {
		detailLines = append(detailLines, email)
	}
	for _, detailLine := range detailLines {
		companyText = append(companyText, text.New(truncateToWidth(detailLine, textWidth, 8.5), props.Text{Size: 8.5, Color: pdfMutedText, Align: align.Center, Top: nextTop}))
		nextTop += 4.0
	}

	letterheadRow := row.New(max(20.0, nextTop+1))
	if showLogo {
		letterheadRow.Add(
			image.NewFromBytesCol(invoiceLogoColumns, writer.branding.LogoPng, extension.Png, props.Rect{Percent: 95, Center: true}),
			col.New(textColumns).Add(companyText...),
			col.New(invoiceLogoColumns),
		)
	} else {
		letterheadRow.Add(col.New(textColumns).Add(companyText...))
	}

	brandRule := row.New(0.9).Add(col.New(pdfGridSize)).WithStyle(&props.Cell{BackgroundColor: writer.brandColor})
	return []core.Row{letterheadRow, row.New(1.5), brandRule, row.New(4)}
}

func titleSizeFor(titleText string) float64 {
	if utf8.RuneCountInString(titleText) > 18 {
		return 13
	}
	return 16
}

func (writer *invoicePdfWriter) addTitle() {
	titleText := strings.ToUpper(writer.invoice.Title)
	titleRow := row.New(9).Add(
		col.New(invoiceStampColumns),
		text.NewCol(pdfGridSize-2*invoiceStampColumns, titleText, props.Text{Size: titleSizeFor(titleText), Style: fontstyle.Bold, Align: align.Center, Top: 1.6}),
	)
	if writer.invoice.Status != "" && !writer.hides(InvoicePartStatus) {
		stampColor := invoiceToneColors[writer.invoice.StatusTone]
		titleRow.Add(text.NewCol(invoiceStampColumns, strings.ToUpper(writer.invoice.Status), props.Text{Size: 9, Style: fontstyle.Bold, Color: stampColor, Align: align.Center, Top: 2.6}).
			WithStyle(&props.Cell{BorderType: border.Full, BorderColor: stampColor, BorderThickness: 0.7}))
	} else {
		titleRow.Add(col.New(invoiceStampColumns))
	}
	writer.add(titleRow)
	writer.add(row.New(4))
}

func (writer *invoicePdfWriter) documentFacts() []Field {
	facts := []Field{}
	if writer.invoice.Number != "" {
		numberLabel := writer.invoice.Title + " " + Label(writer.language, "number")
		if utf8.RuneCountInString(numberLabel) > 18 {
			numberLabel = Label(writer.language, "documentNumber")
		}
		facts = append(facts, Field{Label: numberLabel, Value: writer.invoice.Number, Strong: true})
	}
	facts = append(facts, writer.invoice.Facts...)
	if writer.branding.CurrencyCode != "" {
		facts = append(facts, Field{Label: Label(writer.language, "currency"), Value: writer.branding.CurrencyCode})
	}
	if !writer.hides(InvoicePartTaxIds) {
		if writer.branding.Tin != "" {
			facts = append(facts, Field{Label: Label(writer.language, "tin"), Value: writer.branding.Tin})
		}
		if writer.branding.Vrn != "" {
			facts = append(facts, Field{Label: Label(writer.language, "vrn"), Value: writer.branding.Vrn})
		}
	}
	return facts
}

func (writer *invoicePdfWriter) addPartyAndFacts() {
	factColumns := pdfGridSize - invoicePartyColumns - invoiceGapColumns
	factValueColumns := factColumns - invoiceFactLabel
	partyWidth := writer.columnWidth(invoicePartyColumns) - 2*invoiceCellPadding

	partyLines := []invoiceLeftLine{}
	for _, nameLine := range wrapText(writer.invoice.Party.Name, partyWidth, 10, pdfBoldCharEm) {
		partyLines = append(partyLines, invoiceLeftLine{text: nameLine, style: props.Text{Size: 10, Style: fontstyle.Bold, Top: 1.2, Left: invoiceCellPadding}})
	}
	for _, partyLine := range writer.invoice.Party.Lines {
		for _, wrappedLine := range wrapText(partyLine, partyWidth, invoiceBodyFontSize, pdfAverageCharEm) {
			partyLines = append(partyLines, invoiceLeftLine{text: wrappedLine, style: props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText, Top: 1.4, Left: invoiceCellPadding}})
		}
	}
	facts := writer.documentFacts()
	rowCount := max(len(partyLines), len(facts), 3)

	headingHeight := 5.0
	writer.keepTogether(headingHeight + float64(rowCount)*invoiceFactRowHeight)
	writer.add(row.New(headingHeight).Add(
		text.NewCol(invoicePartyColumns, writer.invoice.Party.Heading+":", props.Text{Size: 8, Style: fontstyle.Bold, Top: 0.8}),
		col.New(pdfGridSize-invoicePartyColumns),
	))

	for rowIndex := range rowCount {
		partyBorder := border.Left | border.Right
		if rowIndex == 0 {
			partyBorder |= border.Top
		}
		if rowIndex == rowCount-1 {
			partyBorder |= border.Bottom
		}
		partyColumn := col.New(invoicePartyColumns).WithStyle(writer.gridCell(partyBorder))
		if rowIndex < len(partyLines) {
			partyColumn.Add(text.New(partyLines[rowIndex].text, partyLines[rowIndex].style))
		}
		factsRow := row.New(invoiceFactRowHeight).Add(partyColumn, col.New(invoiceGapColumns))

		if rowIndex >= len(facts) {
			writer.add(factsRow.Add(col.New(factColumns)))
			continue
		}
		fact := facts[rowIndex]
		factValue := FormatValue(writer.branding, writer.language, fact.Kind, fact.Value)
		if fact.Kind == Money {
			factValue = FormatCurrency(moneyOf(fact.Value), writer.branding)
		}
		valueStyle := fontstyle.Normal
		if fact.Strong {
			valueStyle = fontstyle.Bold
		}
		labelCell := writer.gridCell(border.Full)
		labelCell.BackgroundColor = invoiceHeadFill
		factLabel := truncateToWidth(fact.Label, writer.columnWidth(invoiceFactLabel)-2*invoiceCellPadding, invoiceSmallFontSize)
		factValue = truncateToWidth(factValue, writer.columnWidth(factValueColumns)-2*invoiceCellPadding, invoiceBodyFontSize)
		writer.add(factsRow.Add(
			text.NewCol(invoiceFactLabel, factLabel, props.Text{Size: invoiceSmallFontSize, Color: pdfMutedText, Top: 1.7, Left: invoiceCellPadding}).WithStyle(labelCell),
			text.NewCol(factValueColumns, factValue, props.Text{Size: invoiceBodyFontSize, Style: valueStyle, Top: 1.4, Right: invoiceCellPadding, Align: align.Right}).WithStyle(writer.gridCell(border.Full)),
		))
	}
	writer.add(row.New(3))
}

func (writer *invoicePdfWriter) addIssuedBy() {
	if writer.invoice.PreparedBy == "" || writer.hides(InvoicePartIssuedBy) {
		return
	}
	location := writer.branding.Location()
	issuedParts := []string{Label(writer.language, "preparedBy") + ": " + strings.ToUpper(writer.invoice.PreparedBy)}
	if !writer.invoice.PreparedAt.IsZero() {
		issuedParts = append(issuedParts, Label(writer.language, "postedAt")+": "+FormatDateTime(writer.language, writer.invoice.PreparedAt.In(location)))
	}
	issuedParts = append(issuedParts, Label(writer.language, "printedAt")+": "+FormatDateTime(writer.language, writer.invoice.GeneratedAt.In(location)))
	writer.add(text.NewRow(5.5, truncateToWidth(strings.Join(issuedParts, "      "), writer.contentWidth, 7.8), props.Text{Size: 7.8, Color: pdfMutedText, Top: 0.8}))
	writer.add(row.New(1.5))
}

func (writer *invoicePdfWriter) lineColumns() []invoiceColumn {
	money := func(value int64) string { return FormatMoney(value, writer.branding.CurrencyDecimals) }
	quantity := invoiceColumn{title: Label(writer.language, "quantity"), size: 9, alignment: align.Center, value: func(invoiceLine InvoiceLine) string { return FormatInteger(invoiceLine.Quantity) }}
	description := invoiceColumn{title: Label(writer.language, "description"), alignment: align.Left}
	unit := invoiceColumn{title: Label(writer.language, "unit"), size: 9, alignment: align.Center, value: func(invoiceLine InvoiceLine) string { return invoiceLine.Unit }}
	showUnit := !writer.hides(InvoicePartUnit)

	columns := []invoiceColumn{quantity, description}
	if writer.invoice.HidePrices {
		if showUnit {
			columns = append(columns, unit)
		}
		columns = append(columns, invoiceColumn{title: Label(writer.language, "received"), size: 14, alignment: align.Center, value: func(invoiceLine InvoiceLine) string { return "" }})
		return withDescriptionWidth(columns)
	}

	columns = append(columns, invoiceColumn{title: Label(writer.language, "unitPrice"), size: 15, alignment: align.Right, value: func(invoiceLine InvoiceLine) string { return money(invoiceLine.UnitPrice) }})
	if showUnit {
		columns = append(columns, unit)
	}
	hasDiscount := false
	for _, invoiceLine := range writer.invoice.Lines {
		hasDiscount = hasDiscount || invoiceLine.Discount != 0
	}
	if hasDiscount && !writer.hides(InvoicePartDiscount) {
		columns = append(columns, invoiceColumn{title: Label(writer.language, "discount"), size: 12, alignment: align.Right, value: func(invoiceLine InvoiceLine) string {
			if invoiceLine.Discount == 0 {
				return ""
			}
			return "-" + money(invoiceLine.Discount)
		}})
	}
	columns = append(columns, invoiceColumn{title: Label(writer.language, "amount"), size: 16, alignment: align.Right, value: func(invoiceLine InvoiceLine) string { return money(invoiceLine.Amount) }})
	return withDescriptionWidth(columns)
}

func withDescriptionWidth(columns []invoiceColumn) []invoiceColumn {
	usedColumns := 0
	for _, column := range columns {
		usedColumns += column.size
	}
	for columnIndex := range columns {
		if columns[columnIndex].value == nil {
			columns[columnIndex].size = pdfGridSize - usedColumns
		}
	}
	return columns
}

func (writer *invoicePdfWriter) addLines() {
	columns := writer.lineColumns()
	headerHeight := 7.0
	addHeader := func() {
		headerRow := row.New(headerHeight)
		for _, column := range columns {
			headerCell := writer.gridCell(border.Full)
			headerCell.BackgroundColor = invoiceHeadFill
			headerRow.Add(text.NewCol(column.size, strings.ToUpper(column.title), props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Top: 2.2, Left: invoiceCellPadding, Right: invoiceCellPadding, Align: column.alignment}).WithStyle(headerCell))
		}
		writer.add(headerRow)
	}

	writer.keepTogether(headerHeight + 3*(invoiceLineHeight+invoiceRowPadding))
	addHeader()
	if len(writer.invoice.Lines) == 0 {
		writer.add(text.NewRow(8, Label(writer.language, "nothingToShow"), props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText, Top: 2.5, Left: invoiceCellPadding}).WithStyle(writer.gridCell(border.Full)))
	}

	descriptionWidth := 0.0
	for _, column := range columns {
		if column.value == nil {
			descriptionWidth = writer.columnWidth(column.size) - 2*invoiceCellPadding
		}
	}
	showSku := !writer.hides(InvoicePartSku)
	for _, invoiceLine := range writer.invoice.Lines {
		descriptionLines := wrapText(invoiceLine.Description, descriptionWidth, invoiceBodyFontSize, pdfAverageCharEm)
		detailLines := []string{}
		if showSku {
			detailLines = wrapText(invoiceLine.Detail, descriptionWidth, invoiceSmallFontSize, pdfAverageCharEm)
		}
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
					descriptionComponents = append(descriptionComponents, text.New(descriptionLine, props.Text{Size: invoiceBodyFontSize, Top: lineTop, Left: invoiceCellPadding}))
					lineTop += invoiceLineHeight
				}
				for _, detailLine := range detailLines {
					descriptionComponents = append(descriptionComponents, text.New(detailLine, props.Text{Size: invoiceSmallFontSize, Color: pdfMutedText, Top: lineTop, Left: invoiceCellPadding}))
					lineTop += 3.4
				}
				lineRow.Add(col.New(column.size).Add(descriptionComponents...).WithStyle(writer.gridCell(border.Full)))
				continue
			}
			cellText := truncateToWidth(column.value(invoiceLine), writer.columnWidth(column.size)-2*invoiceCellPadding, invoiceBodyFontSize)
			lineRow.Add(text.NewCol(column.size, cellText, props.Text{Size: invoiceBodyFontSize, Top: invoiceRowPadding / 2, Left: invoiceCellPadding, Right: invoiceCellPadding, Align: column.alignment}).WithStyle(writer.gridCell(border.Full)))
		}
		writer.add(lineRow)
	}
	writer.add(row.New(4))
}

func mutedWhen(isMuted bool) *props.Color {
	if isMuted {
		return pdfMutedText
	}
	return pdfDarkText
}

func (writer *invoicePdfWriter) addTotalsBlock() {
	totals := writer.invoice.Totals
	if len(totals) == 0 {
		return
	}
	leftColumns := pdfGridSize - invoiceTotalsColumns - invoiceGapColumns
	leftWidth := writer.columnWidth(leftColumns)

	leftLines := []invoiceLeftLine{{text: Label(writer.language, "errorsExcepted"), style: props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: pdfMutedText}, height: 6}}
	if words := strings.TrimSpace(writer.invoice.AmountInWords); words != "" && !writer.hides(InvoicePartAmountInWords) {
		leftLines = append(leftLines, invoiceLeftLine{text: strings.ToUpper(Label(writer.language, "amountInWords")), style: props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor}, height: 4.4})
		for _, wordsLine := range wrapText(words, leftWidth, 9, pdfBoldCharEm) {
			leftLines = append(leftLines, invoiceLeftLine{text: wordsLine, style: props.Text{Size: 9, Style: fontstyle.BoldItalic}, height: 4.6})
		}
	}
	takeLeftLines := func(rowHeight float64) []core.Component {
		components := []core.Component{}
		usedHeight := 0.0
		for len(leftLines) > 0 && usedHeight+leftLines[0].height <= rowHeight+0.01 {
			lineStyle := leftLines[0].style
			lineStyle.Top = usedHeight + 1
			components = append(components, text.New(leftLines[0].text, lineStyle))
			usedHeight += leftLines[0].height
			leftLines = leftLines[1:]
		}
		return components
	}

	blockHeight := 0.0
	for _, total := range totals {
		blockHeight += invoiceTotalRow
		if total.Strong {
			blockHeight += invoiceStrongRow - invoiceTotalRow
		}
	}
	writer.keepTogether(blockHeight)

	valueColumns := invoiceTotalsColumns - invoiceTotalsLabel
	for _, total := range totals {
		rowHeight, fontSize, fontStyle := invoiceTotalRow, invoiceBodyFontSize, fontstyle.Normal
		labelCell, valueCell := writer.gridCell(border.Full), writer.gridCell(border.Full)
		if total.Strong {
			rowHeight, fontSize, fontStyle = invoiceStrongRow, 10, fontstyle.Bold
			labelCell.BackgroundColor, valueCell.BackgroundColor = invoiceHeadFill, invoiceHeadFill
		}
		textTop := (rowHeight - fontSize*pdfPointToMm) / 2
		writer.add(row.New(rowHeight).Add(
			col.New(leftColumns).Add(takeLeftLines(rowHeight)...),
			col.New(invoiceGapColumns),
			text.NewCol(invoiceTotalsLabel, total.Label, props.Text{Size: fontSize - 0.5, Style: fontStyle, Color: mutedWhen(!total.Strong), Top: textTop, Left: invoiceCellPadding}).WithStyle(labelCell),
			text.NewCol(valueColumns, writer.formatTotal(total), props.Text{Size: fontSize, Style: fontStyle, Top: textTop, Right: invoiceCellPadding, Align: align.Right}).WithStyle(valueCell),
		))
	}
	for len(leftLines) > 0 {
		lineRowHeight := leftLines[0].height
		writer.add(row.New(lineRowHeight + 1).Add(col.New(leftColumns).Add(takeLeftLines(lineRowHeight)...)))
	}
	writer.add(row.New(6))
}

func (writer *invoicePdfWriter) formatTotal(total Field) string {
	if total.Kind == Money {
		return FormatCurrency(moneyOf(total.Value), writer.branding)
	}
	return FormatValue(writer.branding, writer.language, total.Kind, total.Value)
}

func (writer *invoicePdfWriter) addTermsAndPayment() {
	leftColumns := pdfGridSize - invoiceTotalsColumns - invoiceGapColumns
	sectionLines := func(heading string, body string, columns int) []invoiceLeftLine {
		if strings.TrimSpace(body) == "" {
			return nil
		}
		lines := []invoiceLeftLine{{text: strings.ToUpper(heading), style: props.Text{Size: invoiceSmallFontSize, Style: fontstyle.Bold, Color: writer.brandColor}, height: 4.6}}
		for _, paragraph := range strings.Split(body, "\n") {
			for _, bodyLine := range wrapText(paragraph, writer.columnWidth(columns), invoiceBodyFontSize, pdfAverageCharEm) {
				lines = append(lines, invoiceLeftLine{text: bodyLine, style: props.Text{Size: invoiceBodyFontSize, Color: pdfMutedText}, height: invoiceLineHeight})
			}
		}
		return lines
	}

	termsLines := []invoiceLeftLine{}
	if !writer.hides(InvoicePartTerms) {
		termsLines = sectionLines(Label(writer.language, "terms"), writer.invoice.Terms, leftColumns)
	}
	paymentLines := []invoiceLeftLine{}
	if !writer.hides(InvoicePartPaymentDetails) {
		paymentLines = sectionLines(Label(writer.language, "paymentDetails"), writer.invoice.PaymentDetails, invoiceTotalsColumns)
	}
	if len(termsLines) > 0 || len(paymentLines) > 0 {
		writer.keepTogether(4.6 + min(float64(max(len(termsLines), len(paymentLines))), 4)*invoiceLineHeight)
		for lineIndex := range max(len(termsLines), len(paymentLines)) {
			lineRow := row.New(invoiceLineHeight + 0.4)
			lineRow.Add(writer.sectionColumn(termsLines, lineIndex, leftColumns), col.New(invoiceGapColumns), writer.sectionColumn(paymentLines, lineIndex, invoiceTotalsColumns))
			writer.add(lineRow)
		}
		writer.add(row.New(4))
	}

	notesLines := sectionLines(Label(writer.language, "statementNotes"), strings.Join(writer.invoice.Notes, "\n"), pdfGridSize)
	for lineIndex := range notesLines {
		writer.add(row.New(invoiceLineHeight + 0.4).Add(writer.sectionColumn(notesLines, lineIndex, pdfGridSize)))
	}
	if len(notesLines) > 0 {
		writer.add(row.New(4))
	}
}

func (writer *invoicePdfWriter) sectionColumn(lines []invoiceLeftLine, lineIndex int, columns int) core.Col {
	sectionColumn := col.New(columns)
	if lineIndex < len(lines) {
		sectionColumn.Add(text.New(lines[lineIndex].text, lines[lineIndex].style))
	}
	return sectionColumn
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
	if len(signatureLabels) == 0 || writer.hides(InvoicePartSignatures) {
		return
	}
	signatureCount := min(len(signatureLabels), 3)
	gapColumns := 6
	signatureColumns := (pdfGridSize - gapColumns*(signatureCount-1)) / signatureCount
	writer.keepTogether(24)
	writer.add(row.New(8))

	lineStyle := &props.Cell{BorderType: border.Bottom, BorderColor: pdfDarkText, BorderThickness: 0.3}
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
