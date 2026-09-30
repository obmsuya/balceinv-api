package accounting

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/google/uuid"
)

var profitAndLossLabels = map[string]map[string]string{
	documents.English: {
		"revenue":        "Revenue",
		"totalRevenue":   "Total revenue",
		"costOfGoods":    "Less: cost of goods sold",
		"grossProfit":    "Gross profit",
		"grossLoss":      "Gross loss",
		"grossMargin":    "Gross margin",
		"expenses":       "Operating expenses",
		"totalExpenses":  "Total operating expenses",
		"netProfit":      "Net profit",
		"netLoss":        "Net loss",
		"netMargin":      "Net margin",
		"noRevenue":      "No sales or other income in this period",
		"noExpenses":     "No expenses recorded in this period",
		"thisPeriod":     "This period",
		"previousPeriod": "Previous period",
		"comparedWith":   "Compared with",
		"sheet":          "Profit and loss",
		"noteBrackets":   "Amounts in brackets reduce profit. A dash means nothing was recorded.",
		"noteVat":        "Sales are shown without VAT. The VAT collected is owed to TRA and is shown in the VAT report.",
		"noteCogs":       "Cost of goods sold is what the items sold cost to buy, at their average cost on the day of each sale.",
		"noteSource":     "Prepared from the double-entry books. Every figure can be traced to its entries in the ledger.",
		"notePartial":    "The books start on {date}, so the previous period may be incomplete.",
		"noteShop":       "Only amounts recorded for this shop are included. Costs recorded for the whole business, such as rent paid centrally, appear under All shops.",
	},
	documents.Swahili: {
		"revenue":        "Mapato",
		"totalRevenue":   "Jumla ya mapato",
		"costOfGoods":    "Toa: gharama ya bidhaa zilizouzwa",
		"grossProfit":    "Faida ghafi",
		"grossLoss":      "Hasara ghafi",
		"grossMargin":    "Asilimia ya faida ghafi",
		"expenses":       "Matumizi ya uendeshaji",
		"totalExpenses":  "Jumla ya matumizi ya uendeshaji",
		"netProfit":      "Faida halisi",
		"netLoss":        "Hasara halisi",
		"netMargin":      "Asilimia ya faida halisi",
		"noRevenue":      "Hakuna mauzo wala mapato mengine katika kipindi hiki",
		"noExpenses":     "Hakuna matumizi yaliyorekodiwa katika kipindi hiki",
		"thisPeriod":     "Kipindi hiki",
		"previousPeriod": "Kipindi kilichopita",
		"comparedWith":   "Ikilinganishwa na",
		"sheet":          "Faida na hasara",
		"noteBrackets":   "Kiasi kilicho kwenye mabano kinapunguza faida. Kistari kinamaanisha hakuna kilichorekodiwa.",
		"noteVat":        "Mauzo yameonyeshwa bila VAT. VAT iliyokusanywa inadaiwa na TRA na inaonyeshwa kwenye ripoti ya VAT.",
		"noteCogs":       "Gharama ya bidhaa zilizouzwa ni bei ya kununua bidhaa hizo, kwa wastani wa gharama siku ya kila mauzo.",
		"noteSource":     "Imeandaliwa kutoka kwenye leja ya hesabu. Kila kiasi kinaweza kufuatiliwa hadi rekodi zake kwenye leja.",
		"notePartial":    "Hesabu zinaanza tarehe {date}, kwa hiyo kipindi kilichopita kinaweza kuwa hakijakamilika.",
		"noteShop":       "Kiasi kilichorekodiwa kwa duka hili pekee ndicho kimejumuishwa. Gharama za biashara nzima, kama kodi ya pango inayolipwa makao makuu, zinaonekana chini ya Maduka yote.",
	},
}

type periodRange struct {
	fromDate time.Time
	toDate   time.Time
}

func (exporting exporter) profitAndLossLabel(key string) string {
	labelText, isKnown := profitAndLossLabels[exporting.language][key]
	if !isKnown {
		return profitAndLossLabels[documents.English][key]
	}
	return labelText
}

func (service *Service) profitAndLossStatement(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Statement, string, error) {
	currentReport, currentError := service.ProfitAndLoss(ctx, querier, principal, request.Range)
	if currentError != nil {
		return documents.Statement{}, "", currentError
	}
	currentPeriod, parseError := parsePeriod(currentReport.FromDate, currentReport.ToDate)
	if parseError != nil {
		return documents.Statement{}, "", parseError
	}
	previousPeriod := precedingPeriod(currentPeriod)
	previousReport, previousError := service.ProfitAndLoss(ctx, querier, principal, ReportRequest{
		FromDate: previousPeriod.fromDate.Format(dateLayout),
		ToDate:   previousPeriod.toDate.Format(dateLayout),
		Shop:     request.Range.Shop,
	})
	if previousError != nil {
		return documents.Statement{}, "", previousError
	}
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return documents.Statement{}, "", booksError
	}
	shopLabel, shopError := service.shopLabel(ctx, querier, principal.CompanyId, request.Range.Shop, exporting)
	if shopError != nil {
		return documents.Statement{}, "", shopError
	}

	statement := documents.Statement{
		Language:        exporting.language,
		Title:           exporting.label("title.profit-and-loss"),
		Subtitle:        documents.PeriodText(exporting.language, currentPeriod.fromDate, currentPeriod.toDate),
		CurrentHeading:  exporting.periodHeading(currentPeriod, "thisPeriod"),
		PreviousHeading: exporting.periodHeading(previousPeriod, "previousPeriod"),
		Filters: []documents.Field{
			{Label: exporting.label("shop"), Value: shopLabel},
			{Label: exporting.profitAndLossLabel("comparedWith"), Value: documents.PeriodText(exporting.language, previousPeriod.fromDate, previousPeriod.toDate)},
		},
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
		Rows:        exporting.profitAndLossRows(currentReport, previousReport),
		Notes:       exporting.profitAndLossNotes(books, previousPeriod, request.Range.Shop != ""),
		SheetName:   exporting.profitAndLossLabel("sheet"),
	}
	fileName := "profit-and-loss-" + currentReport.FromDate + "-to-" + currentReport.ToDate
	return statement, fileName, nil
}

func (exporting exporter) profitAndLossRows(currentReport ProfitAndLossView, previousReport ProfitAndLossView) []documents.StatementRow {
	statementRows := []documents.StatementRow{{Style: documents.StatementHeading, Label: exporting.profitAndLossLabel("revenue")}}
	statementRows = append(statementRows, exporting.accountRows(currentReport.Income, previousReport.Income, exporting.profitAndLossLabel("noRevenue"))...)
	statementRows = append(statementRows,
		documents.StatementRow{Key: "revenue", Style: documents.StatementSubtotal, Label: exporting.profitAndLossLabel("totalRevenue"), Current: currentReport.TotalIncome, Previous: previousReport.TotalIncome, SumsSection: true},
		documents.StatementRow{Key: "cogs", Style: documents.StatementLine, Label: exporting.profitAndLossLabel("costOfGoods"), Current: -currentReport.CostOfGoods, Previous: -previousReport.CostOfGoods, IsDeduction: true},
		documents.StatementRow{
			Key:      "gross",
			Style:    documents.StatementTotal,
			Label:    exporting.profitAndLossLabel(profitOrLoss(currentReport.GrossProfit, "grossProfit", "grossLoss")),
			Current:  currentReport.GrossProfit,
			Previous: previousReport.GrossProfit,
			Terms:    []documents.StatementTerm{{Key: "revenue"}, {Key: "cogs"}},
		},
		documents.StatementRow{Style: documents.StatementRatio, Label: exporting.profitAndLossLabel("grossMargin"), RatioNumerator: "gross", RatioDenominator: "revenue"},
		documents.StatementRow{Style: documents.StatementHeading, Label: exporting.profitAndLossLabel("expenses")},
	)
	statementRows = append(statementRows, exporting.accountRows(currentReport.Expenses, previousReport.Expenses, exporting.profitAndLossLabel("noExpenses"))...)
	statementRows = append(statementRows,
		documents.StatementRow{Key: "expenses", Style: documents.StatementSubtotal, Label: exporting.profitAndLossLabel("totalExpenses"), Current: currentReport.TotalExpenses, Previous: previousReport.TotalExpenses, SumsSection: true},
		documents.StatementRow{
			Key:      "net",
			Style:    documents.StatementGrandTotal,
			Label:    exporting.profitAndLossLabel(profitOrLoss(currentReport.NetProfit, "netProfit", "netLoss")),
			Current:  currentReport.NetProfit,
			Previous: previousReport.NetProfit,
			Terms:    []documents.StatementTerm{{Key: "gross"}, {Key: "expenses", Subtract: true}},
		},
		documents.StatementRow{Style: documents.StatementRatio, Label: exporting.profitAndLossLabel("netMargin"), RatioNumerator: "net", RatioDenominator: "revenue"},
	)
	return statementRows
}

func (exporting exporter) accountRows(currentAmounts []AccountAmountView, previousAmounts []AccountAmountView, emptyLabel string) []documents.StatementRow {
	accountsById := map[uuid.UUID]AccountAmountView{}
	currentById := map[uuid.UUID]int64{}
	previousById := map[uuid.UUID]int64{}
	for _, currentAmount := range currentAmounts {
		accountsById[currentAmount.AccountId] = currentAmount
		currentById[currentAmount.AccountId] = currentAmount.Amount
	}
	for _, previousAmount := range previousAmounts {
		if _, isKnown := accountsById[previousAmount.AccountId]; !isKnown {
			accountsById[previousAmount.AccountId] = previousAmount
		}
		previousById[previousAmount.AccountId] = previousAmount.Amount
	}

	orderedAccounts := []AccountAmountView{}
	for _, account := range accountsById {
		orderedAccounts = append(orderedAccounts, account)
	}
	sort.Slice(orderedAccounts, func(leftIndex int, rightIndex int) bool {
		leftAccount, rightAccount := orderedAccounts[leftIndex], orderedAccounts[rightIndex]
		if leftAccount.Code != rightAccount.Code {
			return leftAccount.Code < rightAccount.Code
		}
		return exporting.accountName("", leftAccount.SystemKey, leftAccount.Name) < exporting.accountName("", rightAccount.SystemKey, rightAccount.Name)
	})

	statementRows := []documents.StatementRow{}
	for _, account := range orderedAccounts {
		statementRows = append(statementRows, documents.StatementRow{
			Style:    documents.StatementItem,
			Code:     account.Code,
			Label:    exporting.accountName("", account.SystemKey, account.Name),
			Current:  currentById[account.AccountId],
			Previous: previousById[account.AccountId],
		})
	}
	if len(statementRows) == 0 {
		statementRows = append(statementRows, documents.StatementRow{Style: documents.StatementEmpty, Label: emptyLabel})
	}
	return statementRows
}

func (exporting exporter) profitAndLossNotes(books Books, previousPeriod periodRange, isOneShop bool) []string {
	notes := []string{exporting.profitAndLossLabel("noteBrackets")}
	if isOneShop {
		notes = append(notes, exporting.profitAndLossLabel("noteShop"))
	}
	if exporting.branding.VatRegistered || books.VatRegistered {
		notes = append(notes, exporting.profitAndLossLabel("noteVat"))
	}
	notes = append(notes, exporting.profitAndLossLabel("noteCogs"), exporting.profitAndLossLabel("noteSource"))
	booksStart, startError := time.Parse(dateLayout, books.StartedOn)
	if startError == nil && previousPeriod.fromDate.Before(booksStart) {
		notes = append(notes, strings.ReplaceAll(exporting.profitAndLossLabel("notePartial"), "{date}", documents.FormatDate(exporting.language, booksStart)))
	}
	return notes
}

func (exporting exporter) periodHeading(period periodRange, fallbackKey string) string {
	return documents.PeriodHeading(exporting.language, period.fromDate, period.toDate, fallbackKey == "previousPeriod")
}

func (service *Service) shopLabel(ctx context.Context, querier database.Querier, companyId uuid.UUID, rawShopId string, exporting exporter) (string, error) {
	shopId, shopError := service.optionalShop(ctx, querier, companyId, &rawShopId)
	if shopError != nil {
		return "", shopError
	}
	if shopId == nil {
		return exporting.label("allShops"), nil
	}
	return service.repository.ShopName(ctx, querier, companyId, *shopId)
}

func parsePeriod(fromDate string, toDate string) (periodRange, error) {
	firstDay, firstError := time.Parse(dateLayout, fromDate)
	if firstError != nil {
		return periodRange{}, ErrInvalidDate
	}
	lastDay, lastError := time.Parse(dateLayout, toDate)
	if lastError != nil {
		return periodRange{}, ErrInvalidDate
	}
	return periodRange{fromDate: firstDay, toDate: lastDay}, nil
}

func precedingPeriod(period periodRange) periodRange {
	previousFrom, previousTo := documents.PreviousPeriod(period.fromDate, period.toDate)
	return periodRange{fromDate: previousFrom, toDate: previousTo}
}

func profitOrLoss(amount int64, profitKey string, lossKey string) string {
	if amount < 0 {
		return lossKey
	}
	return profitKey
}
