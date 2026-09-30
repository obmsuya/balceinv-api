package accounting

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
)

var booksDocumentLabels = map[string]map[string]string{
	documents.English: {
		"title.overview":     "Money summary",
		"sheet.balanceSheet": "Balance sheet",
		"sheet.trialBalance": "Trial balance",
		"sheet.statement":    "Account statement",
		"sheet.vat":          "VAT return",
		"sheet.overview":     "Money summary",
		"asAtDate":           "As at {date}",
		"comparedWith":       "Compared with",
		"nothingRecorded":    "Nothing recorded",
		"bsNoteBalanced":     "Assets equal liabilities plus equity, so the books balance.",
		"bsNoteUnbalanced":   "The books are out by {amount}. Check the trial balance before relying on this statement.",
		"bsNoteStock":        "Stock is valued at its average buying cost.",
		"bsNoteProfit":       "Profit to date is the profit made since the books started that has not yet been closed into retained profit.",
		"ovWhereMoneyIs":     "Where your money is",
		"ovTotalMoney":       "Total money",
		"ovFlow":             "Money in and out",
		"ovMoneyIn":          "Money in",
		"ovMoneyOut":         "Less: money out",
		"ovNetFlow":          "Net money in",
		"ovNetFlowOut":       "Net money out",
		"ovProfitSection":    "Profit",
		"ovIncome":           "Income",
		"ovCosts":            "Less: costs",
		"ovProfit":           "Profit",
		"ovLoss":             "Loss",
		"ovOwed":             "Who owes whom",
		"ovCustomersOwe":     "Customers owe you",
		"ovYouOweSuppliers":  "You owe suppliers",
		"ovWorth":            "What the business is worth",
		"ovOwn":              "What you own",
		"ovOwe":              "Less: what you owe",
		"ovNetWorth":         "Net worth",
		"ovVat":              "VAT",
		"ovVatToPay":         "VAT to pay by {date}",
		"ovAmount":           "Amount",
		"ovNoteDates":        "Money, what customers owe, what you owe and net worth are as at {date}. Money in, money out and profit are for the period.",
		"ovNoteMoneyIn":      "Money in and out leave out money moved between your own accounts and the starting balances.",
		"tbTotal":            "Total",
		"tbNoteBalanced":     "Total debits equal total credits.",
		"tbNoteUnbalanced":   "Debits and credits differ by {amount}. Tell Balce support.",
		"tbNoteNormal":       "Each account shows its balance on its normal side: assets and costs as debits, liabilities, equity and income as credits.",
		"tbEmpty":            "No entries yet",
		"stTotal":            "Total for the period",
		"stEmpty":            "No entries in this period",
		"stOpeningSummary":   "Opening balance",
		"stClosingSummary":   "Closing balance",
		"stNoteSource":       "Every line is an entry in the ledger; its number matches the entry in Balce.",
		"vatTotal":           "Total",
		"vatEmpty":           "No VAT in this period",
		"vatNoteDue":         "VAT is paid to TRA by the 20th of the following month.",
		"vatNoteCredit":      "A negative amount to pay is VAT you can carry forward to the next month.",
		"vatNoteSource":      "VAT charged comes from sales; VAT reclaimable comes from purchases and expenses entered with a TRA receipt.",
	},
	documents.Swahili: {
		"title.overview":     "Muhtasari wa fedha",
		"sheet.balanceSheet": "Mizania",
		"sheet.trialBalance": "Mizania ya majaribio",
		"sheet.statement":    "Mwenendo wa akaunti",
		"sheet.vat":          "Ritani ya VAT",
		"sheet.overview":     "Muhtasari wa fedha",
		"asAtDate":           "Kufikia {date}",
		"comparedWith":       "Ikilinganishwa na",
		"nothingRecorded":    "Hakuna kilichorekodiwa",
		"bsNoteBalanced":     "Mali ni sawa na madeni pamoja na mtaji, kwa hiyo hesabu zinalingana.",
		"bsNoteUnbalanced":   "Hesabu zimetofautiana kwa {amount}. Kagua mizania ya majaribio kabla ya kutegemea ripoti hii.",
		"bsNoteStock":        "Stoku imethaminiwa kwa wastani wa bei ya kununua.",
		"bsNoteProfit":       "Faida hadi sasa ni faida iliyopatikana tangu hesabu zianze ambayo bado haijafungwa kwenye faida iliyobakizwa.",
		"ovWhereMoneyIs":     "Pesa yako iko wapi",
		"ovTotalMoney":       "Jumla ya pesa",
		"ovFlow":             "Pesa iliyoingia na kutoka",
		"ovMoneyIn":          "Pesa iliyoingia",
		"ovMoneyOut":         "Toa: pesa iliyotoka",
		"ovNetFlow":          "Pesa halisi iliyoingia",
		"ovNetFlowOut":       "Pesa halisi iliyotoka",
		"ovProfitSection":    "Faida",
		"ovIncome":           "Mapato",
		"ovCosts":            "Toa: gharama",
		"ovProfit":           "Faida",
		"ovLoss":             "Hasara",
		"ovOwed":             "Nani anadaiwa",
		"ovCustomersOwe":     "Wateja wanaodaiwa",
		"ovYouOweSuppliers":  "Madeni kwa wasambazaji",
		"ovWorth":            "Thamani ya biashara",
		"ovOwn":              "Unachomiliki",
		"ovOwe":              "Toa: unachodaiwa",
		"ovNetWorth":         "Thamani halisi",
		"ovVat":              "VAT",
		"ovVatToPay":         "VAT ya kulipa kabla ya {date}",
		"ovAmount":           "Kiasi",
		"ovNoteDates":        "Pesa, madeni ya wateja, unachodaiwa na thamani halisi ni kufikia {date}. Pesa iliyoingia, iliyotoka na faida ni za kipindi hiki.",
		"ovNoteMoneyIn":      "Pesa iliyoingia na kutoka haihusishi pesa iliyohamishwa kati ya akaunti zako wala salio la kuanzia.",
		"tbTotal":            "Jumla",
		"tbNoteBalanced":     "Jumla ya debiti ni sawa na jumla ya krediti.",
		"tbNoteUnbalanced":   "Debiti na krediti zimetofautiana kwa {amount}. Wasiliana na msaada wa Balce.",
		"tbNoteNormal":       "Kila akaunti inaonyesha salio lake upande wake wa kawaida: mali na gharama kama debiti, madeni, mtaji na mapato kama krediti.",
		"tbEmpty":            "Bado hakuna rekodi",
		"stTotal":            "Jumla ya kipindi",
		"stEmpty":            "Hakuna rekodi katika kipindi hiki",
		"stOpeningSummary":   "Salio la kuanzia",
		"stClosingSummary":   "Salio la mwisho",
		"stNoteSource":       "Kila mstari ni rekodi kwenye leja; namba yake inalingana na rekodi ndani ya Balce.",
		"vatTotal":           "Jumla",
		"vatEmpty":           "Hakuna VAT katika kipindi hiki",
		"vatNoteDue":         "VAT hulipwa TRA kabla ya tarehe 20 ya mwezi unaofuata.",
		"vatNoteCredit":      "Kiasi hasi cha kulipa ni VAT unayoweza kuipeleka mwezi unaofuata.",
		"vatNoteSource":      "VAT iliyotozwa inatokana na mauzo; VAT ya kurudishiwa inatokana na manunuzi na matumizi yaliyorekodiwa na risiti ya TRA.",
	},
}

func (exporting exporter) booksLabel(key string) string {
	labelText, isKnown := booksDocumentLabels[exporting.language][key]
	if !isKnown {
		return booksDocumentLabels[documents.English][key]
	}
	return labelText
}

func (exporting exporter) amountText(minorUnits int64) string {
	return documents.FormatCurrency(minorUnits, exporting.branding)
}

func (exporting exporter) longDate(date time.Time) string {
	return documents.FormatDate(exporting.language, date)
}

func (service *Service) balanceSheetStatement(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Statement, string, error) {
	current, currentError := service.BalanceSheet(ctx, querier, principal, request.Range.ToDate)
	if currentError != nil {
		return documents.Statement{}, "", currentError
	}
	asOf, parseError := time.Parse(dateLayout, current.AsOf)
	if parseError != nil {
		return documents.Statement{}, "", ErrInvalidDate
	}
	comparisonDate := time.Date(asOf.Year(), asOf.Month(), 0, 0, 0, 0, 0, time.UTC)
	if request.Range.FromDate != "" {
		periodStart, startError := time.Parse(dateLayout, request.Range.FromDate)
		if startError != nil {
			return documents.Statement{}, "", ErrInvalidDate
		}
		comparisonDate = periodStart.AddDate(0, 0, -1)
	}
	previous, previousError := service.BalanceSheet(ctx, querier, principal, comparisonDate.Format(dateLayout))
	if previousError != nil {
		return documents.Statement{}, "", previousError
	}

	equityCurrent := append([]AccountAmountView{}, current.Equity...)
	equityPrevious := append([]AccountAmountView{}, previous.Equity...)
	profitName := exporting.label("profitToDate")
	profitRow := documents.StatementRow{Style: documents.StatementItem, Label: profitName, Current: current.ProfitToDate, Previous: previous.ProfitToDate}

	statementRows := []documents.StatementRow{{Style: documents.StatementHeading, Label: exporting.label("assets")}}
	statementRows = append(statementRows, exporting.accountRows(current.Assets, previous.Assets, exporting.booksLabel("nothingRecorded"))...)
	statementRows = append(statementRows,
		documents.StatementRow{Key: "assets", Style: documents.StatementTotal, Label: exporting.label("totalAssets"), Current: current.TotalAssets, Previous: previous.TotalAssets, SumsSection: true},
		documents.StatementRow{Style: documents.StatementHeading, Label: exporting.label("liabilities")},
	)
	statementRows = append(statementRows, exporting.accountRows(current.Liabilities, previous.Liabilities, exporting.booksLabel("nothingRecorded"))...)
	statementRows = append(statementRows,
		documents.StatementRow{Key: "liabilities", Style: documents.StatementSubtotal, Label: exporting.label("totalLiabilities"), Current: current.TotalLiabilities, Previous: previous.TotalLiabilities, SumsSection: true},
		documents.StatementRow{Style: documents.StatementHeading, Label: exporting.label("equity")},
	)
	equityRows := exporting.accountRows(equityCurrent, equityPrevious, exporting.booksLabel("nothingRecorded"))
	if len(equityRows) == 1 && equityRows[0].Style == documents.StatementEmpty {
		equityRows = nil
	}
	equityRows = append(equityRows, profitRow)
	statementRows = append(statementRows, equityRows...)
	statementRows = append(statementRows,
		documents.StatementRow{Key: "equity", Style: documents.StatementSubtotal, Label: exporting.label("totalEquity"), Current: current.TotalEquity, Previous: previous.TotalEquity, SumsSection: true},
		documents.StatementRow{Style: documents.StatementGrandTotal, Label: exporting.label("liabilitiesAndEquity"), Current: current.TotalLiabilities + current.TotalEquity, Previous: previous.TotalLiabilities + previous.TotalEquity, Terms: []documents.StatementTerm{{Key: "liabilities"}, {Key: "equity"}}},
	)

	notes := []string{}
	if current.IsBalanced {
		notes = append(notes, exporting.booksLabel("bsNoteBalanced"))
	} else {
		difference := current.TotalAssets - current.TotalLiabilities - current.TotalEquity
		notes = append(notes, strings.ReplaceAll(exporting.booksLabel("bsNoteUnbalanced"), "{amount}", exporting.amountText(difference)))
	}
	notes = append(notes, exporting.booksLabel("bsNoteStock"), exporting.booksLabel("bsNoteProfit"), exporting.profitAndLossLabel("noteSource"))

	statement := documents.Statement{
		Language:        exporting.language,
		Title:           exporting.label("title.balance-sheet"),
		Subtitle:        strings.ReplaceAll(exporting.booksLabel("asAtDate"), "{date}", exporting.longDate(asOf)),
		CurrentHeading:  exporting.longDate(asOf),
		PreviousHeading: exporting.longDate(comparisonDate),
		Filters: []documents.Field{
			{Label: exporting.label("shop"), Value: exporting.label("allShops")},
			{Label: exporting.booksLabel("comparedWith"), Value: strings.ReplaceAll(exporting.booksLabel("asAtDate"), "{date}", exporting.longDate(comparisonDate))},
		},
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
		Rows:        statementRows,
		Notes:       notes,
		SheetName:   exporting.booksLabel("sheet.balanceSheet"),
	}
	return statement, "balance-sheet-" + current.AsOf, nil
}

func (service *Service) overviewStatement(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Statement, string, error) {
	overview, overviewError := service.Overview(ctx, querier, principal, request.Range)
	if overviewError != nil {
		return documents.Statement{}, "", overviewError
	}
	period, parseError := parsePeriod(overview.FromDate, overview.ToDate)
	if parseError != nil {
		return documents.Statement{}, "", parseError
	}
	shopLabel, shopError := service.shopLabel(ctx, querier, principal.CompanyId, request.Range.Shop, exporting)
	if shopError != nil {
		return documents.Statement{}, "", shopError
	}

	totalMoney := overview.Balances.Cash + overview.Balances.MobileMoney + overview.Balances.Bank + overview.Balances.CardClearing
	netFlow := overview.MoneyIn - overview.MoneyOut
	netWorth := overview.WhatIOwn - overview.WhatIOwe
	money := func(key string) string {
		return exporting.accountName("", &key, nil)
	}
	statementRows := []documents.StatementRow{
		{Style: documents.StatementHeading, Label: exporting.booksLabel("ovWhereMoneyIs")},
		{Style: documents.StatementItem, Label: money(KeyCash), Current: overview.Balances.Cash},
		{Style: documents.StatementItem, Label: money(KeyMobileMoney), Current: overview.Balances.MobileMoney},
		{Style: documents.StatementItem, Label: money(KeyBank), Current: overview.Balances.Bank},
		{Style: documents.StatementItem, Label: money(KeyCardClearing), Current: overview.Balances.CardClearing},
		{Key: "money", Style: documents.StatementTotal, Label: exporting.booksLabel("ovTotalMoney"), Current: totalMoney, SumsSection: true},
		{Style: documents.StatementHeading, Label: exporting.booksLabel("ovFlow")},
		{Key: "in", Style: documents.StatementLine, Label: exporting.booksLabel("ovMoneyIn"), Current: overview.MoneyIn},
		{Key: "out", Style: documents.StatementLine, Label: exporting.booksLabel("ovMoneyOut"), Current: -overview.MoneyOut, IsDeduction: true},
		{Style: documents.StatementSubtotal, Label: exporting.booksLabel(profitOrLoss(netFlow, "ovNetFlow", "ovNetFlowOut")), Current: netFlow, Terms: []documents.StatementTerm{{Key: "in"}, {Key: "out"}}},
		{Style: documents.StatementHeading, Label: exporting.booksLabel("ovProfitSection")},
		{Key: "income", Style: documents.StatementLine, Label: exporting.booksLabel("ovIncome"), Current: overview.Income},
		{Key: "costs", Style: documents.StatementLine, Label: exporting.booksLabel("ovCosts"), Current: -overview.Costs, IsDeduction: true},
		{Style: documents.StatementTotal, Label: exporting.booksLabel(profitOrLoss(overview.Profit, "ovProfit", "ovLoss")), Current: overview.Profit, Terms: []documents.StatementTerm{{Key: "income"}, {Key: "costs"}}},
		{Style: documents.StatementHeading, Label: exporting.booksLabel("ovOwed")},
		{Style: documents.StatementLine, Label: exporting.booksLabel("ovCustomersOwe"), Current: overview.CustomersOwe},
		{Style: documents.StatementLine, Label: exporting.booksLabel("ovYouOweSuppliers"), Current: overview.OwedToSuppliers},
		{Style: documents.StatementHeading, Label: exporting.booksLabel("ovWorth")},
		{Key: "own", Style: documents.StatementLine, Label: exporting.booksLabel("ovOwn"), Current: overview.WhatIOwn},
		{Key: "owe", Style: documents.StatementLine, Label: exporting.booksLabel("ovOwe"), Current: -overview.WhatIOwe, IsDeduction: true},
		{Style: documents.StatementGrandTotal, Label: exporting.booksLabel("ovNetWorth"), Current: netWorth, Terms: []documents.StatementTerm{{Key: "own"}, {Key: "owe"}}},
	}
	if overview.Vat != nil {
		dueDate, dueError := time.Parse(dateLayout, overview.Vat.DueDate)
		dueText := overview.Vat.DueDate
		if dueError == nil {
			dueText = exporting.longDate(dueDate)
		}
		statementRows = append(statementRows,
			documents.StatementRow{Style: documents.StatementHeading, Label: exporting.booksLabel("ovVat")},
			documents.StatementRow{Key: "vatCharged", Style: documents.StatementLine, Label: exporting.label("vatCharged"), Current: overview.Vat.Charged},
			documents.StatementRow{Key: "vatReclaimable", Style: documents.StatementLine, Label: exporting.label("vatReclaimable"), Current: -overview.Vat.Reclaimable, IsDeduction: true},
			documents.StatementRow{Style: documents.StatementSubtotal, Label: strings.ReplaceAll(exporting.booksLabel("ovVatToPay"), "{date}", dueText), Current: overview.Vat.ToPay, Terms: []documents.StatementTerm{{Key: "vatCharged"}, {Key: "vatReclaimable"}}},
		)
	}

	statement := documents.Statement{
		Language:       exporting.language,
		Title:          exporting.booksLabel("title.overview"),
		Subtitle:       documents.PeriodText(exporting.language, period.fromDate, period.toDate),
		CurrentHeading: exporting.moneyTitle("amount"),
		Filters:        []documents.Field{{Label: exporting.label("shop"), Value: shopLabel}},
		GeneratedBy:    generatedBy,
		GeneratedAt:    time.Now(),
		Rows:           statementRows,
		Notes: []string{
			strings.ReplaceAll(exporting.booksLabel("ovNoteDates"), "{date}", exporting.longDate(period.toDate)),
			exporting.booksLabel("ovNoteMoneyIn"),
			exporting.profitAndLossLabel("noteBrackets"),
		},
		SheetName:    exporting.booksLabel("sheet.overview"),
		SingleColumn: true,
	}
	return statement, "money-summary-" + overview.FromDate + "-to-" + overview.ToDate, nil
}

func (service *Service) trialBalanceRegister(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Register, string, error) {
	report, reportError := service.TrialBalance(ctx, querier, principal, request.Range.ToDate)
	if reportError != nil {
		return documents.Register{}, "", reportError
	}
	asOf, parseError := time.Parse(dateLayout, report.AsOf)
	if parseError != nil {
		return documents.Register{}, "", ErrInvalidDate
	}
	registerRows := []documents.RegisterRow{}
	for _, trialRow := range report.Rows {
		if trialRow.DebitBalance == 0 && trialRow.CreditBalance == 0 {
			continue
		}
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{trialRow.Code, exporting.accountName("", trialRow.SystemKey, trialRow.Name), trialRow.DebitBalance, trialRow.CreditBalance}})
	}
	balanceNote := exporting.booksLabel("tbNoteBalanced")
	if !report.IsBalanced {
		balanceNote = strings.ReplaceAll(exporting.booksLabel("tbNoteUnbalanced"), "{amount}", exporting.amountText(report.TotalDebitBalance-report.TotalCreditBalance))
	}
	register := documents.Register{
		Language:    exporting.language,
		Title:       exporting.label("title.trial-balance"),
		Subtitle:    strings.ReplaceAll(exporting.booksLabel("asAtDate"), "{date}", exporting.longDate(asOf)),
		Filters:     []documents.Field{{Label: exporting.label("shop"), Value: exporting.label("allShops")}},
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
		Columns: []documents.RegisterColumn{
			{Title: exporting.label("code"), Kind: documents.Text, Weight: 0.9},
			{Title: exporting.label("account"), Kind: documents.Text, Weight: 4},
			{Title: exporting.moneyTitle("debit"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("credit"), Kind: documents.Money, Sum: true},
		},
		Rows:       registerRows,
		TotalLabel: exporting.booksLabel("tbTotal"),
		EmptyText:  exporting.booksLabel("tbEmpty"),
		Notes:      []string{balanceNote, exporting.booksLabel("tbNoteNormal")},
		SheetName:  exporting.booksLabel("sheet.trialBalance"),
	}
	return register, "trial-balance-" + report.AsOf, nil
}

func (service *Service) accountStatementRegister(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Register, string, error) {
	report, reportError := service.Statement(ctx, querier, principal, request.Range, request.Account)
	if reportError != nil {
		return documents.Register{}, "", reportError
	}
	period, parseError := parsePeriod(report.FromDate, report.ToDate)
	if parseError != nil {
		return documents.Register{}, "", parseError
	}
	shopLabel, shopError := service.shopLabel(ctx, querier, principal.CompanyId, request.Range.Shop, exporting)
	if shopError != nil {
		return documents.Register{}, "", shopError
	}

	isDebitNormal := report.Account.Type == TypeAsset || report.Account.Type == TypeExpense
	debitEffect, creditEffect := documents.AddsToBalance, documents.TakesFromBalance
	if !isDebitNormal {
		debitEffect, creditEffect = documents.TakesFromBalance, documents.AddsToBalance
	}
	debitTitle, creditTitle := "debit", "credit"
	if report.Account.IsMoney {
		debitTitle, creditTitle = "moneyIn", "moneyOut"
	}
	showsShop := request.Range.Shop == ""
	hasShopNames := false
	for _, line := range report.Lines {
		hasShopNames = hasShopNames || line.ShopName != nil
	}
	showsShop = showsShop && hasShopNames

	columns := []documents.RegisterColumn{
		{Title: exporting.label("date"), Kind: documents.Date},
		{Title: exporting.label("entry"), Kind: documents.Text, Weight: 1.3},
		{Title: exporting.label("description"), Kind: documents.Text, Weight: 3.6},
	}
	if showsShop {
		columns = append(columns, documents.RegisterColumn{Title: exporting.label("shop"), Kind: documents.Text, Weight: 1.6})
	}
	columns = append(columns,
		documents.RegisterColumn{Title: exporting.moneyTitle(debitTitle), Kind: documents.Money, Sum: true, Effect: debitEffect},
		documents.RegisterColumn{Title: exporting.moneyTitle(creditTitle), Kind: documents.Money, Sum: true, Effect: creditEffect},
		documents.RegisterColumn{Title: exporting.moneyTitle("balance"), Kind: documents.Money, Balance: true},
	)

	registerRows := []documents.RegisterRow{}
	for _, line := range report.Lines {
		description := exporting.lineDescription(line)
		cells := []any{exporting.dateCell(line.EntryDate), line.Number, description}
		if showsShop {
			shopName := ""
			if line.ShopName != nil {
				shopName = *line.ShopName
			}
			cells = append(cells, shopName)
		}
		cells = append(cells, zeroAsBlank(line.Debit), zeroAsBlank(line.Credit), line.Balance)
		registerRows = append(registerRows, documents.RegisterRow{Cells: cells})
	}

	openingBalance := report.OpeningBalance
	accountTitle := exporting.accountName(report.Account.Code, report.Account.SystemKey, report.Account.Name)
	register := documents.Register{
		Language:    exporting.language,
		Title:       exporting.label("title.statement"),
		Subtitle:    accountTitle + " · " + documents.PeriodText(exporting.language, period.fromDate, period.toDate),
		Filters:     []documents.Field{{Label: exporting.label("shop"), Value: shopLabel}},
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
		Landscape:   showsShop,
		Summary: []documents.Field{
			{Label: exporting.booksLabel("stOpeningSummary"), Value: report.OpeningBalance, Kind: documents.Money},
			{Label: exporting.label(debitTitle), Value: report.TotalDebit, Kind: documents.Money},
			{Label: exporting.label(creditTitle), Value: report.TotalCredit, Kind: documents.Money},
			{Label: exporting.booksLabel("stClosingSummary"), Value: report.ClosingBalance, Kind: documents.Money, Strong: true},
		},
		Columns:        columns,
		Rows:           registerRows,
		OpeningLabel:   exporting.label("openingBalance"),
		OpeningBalance: &openingBalance,
		TotalLabel:     exporting.booksLabel("stTotal"),
		ClosingLabel:   exporting.label("closingBalance"),
		EmptyText:      exporting.booksLabel("stEmpty"),
		Notes:          []string{exporting.booksLabel("stNoteSource"), exporting.profitAndLossLabel("noteBrackets")},
		SheetName:      exporting.booksLabel("sheet.statement"),
	}
	fileName := "statement-" + firstNonEmptyText(report.Account.Code, "account") + "-" + report.FromDate + "-to-" + report.ToDate
	return register, fileName, nil
}

func (service *Service) vatRegister(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, generatedBy string) (documents.Register, string, error) {
	report, reportError := service.VatReport(ctx, querier, principal, request.Range)
	if reportError != nil {
		return documents.Register{}, "", reportError
	}
	period, parseError := parsePeriod(report.FromDate, report.ToDate)
	if parseError != nil {
		return documents.Register{}, "", parseError
	}
	registerRows := []documents.RegisterRow{}
	hasCredit := false
	for _, month := range report.Months {
		monthStart, monthError := time.Parse("2006-01", month.Month[:min(len(month.Month), 7)])
		monthName := month.Month
		if monthError == nil {
			monthName = documents.MonthYear(exporting.language, monthStart)
		}
		hasCredit = hasCredit || month.ToPay < 0
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{monthName, month.Charged, month.Reclaimable, month.ToPay, exporting.dateCell(month.DueDate)}})
	}
	notes := []string{exporting.booksLabel("vatNoteDue"), exporting.booksLabel("vatNoteSource")}
	if hasCredit {
		notes = append(notes, exporting.booksLabel("vatNoteCredit"))
	}
	vatNumber := exporting.branding.Vrn
	filters := []documents.Field{}
	if vatNumber != "" {
		filters = append(filters, documents.Field{Label: documents.Label(exporting.language, "vrn"), Value: vatNumber})
	}
	register := documents.Register{
		Language:    exporting.language,
		Title:       exporting.label("title.vat"),
		Subtitle:    documents.PeriodText(exporting.language, period.fromDate, period.toDate),
		Filters:     filters,
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
		Summary: []documents.Field{
			{Label: exporting.label("vatCharged"), Value: report.TotalCharged, Kind: documents.Money},
			{Label: exporting.label("vatReclaimable"), Value: report.TotalReclaimable, Kind: documents.Money},
			{Label: exporting.label("vatToPay"), Value: report.TotalToPay, Kind: documents.Money, Strong: true},
		},
		Columns: []documents.RegisterColumn{
			{Title: exporting.label("month"), Kind: documents.Text, Weight: 2},
			{Title: exporting.moneyTitle("vatCharged"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("vatReclaimable"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("vatToPay"), Kind: documents.Money, Sum: true},
			{Title: exporting.label("dueDate"), Kind: documents.Date},
		},
		Rows:       registerRows,
		TotalLabel: exporting.booksLabel("vatTotal"),
		EmptyText:  exporting.booksLabel("vatEmpty"),
		Notes:      notes,
		SheetName:  exporting.booksLabel("sheet.vat"),
	}
	return register, "vat-" + report.FromDate + "-to-" + report.ToDate, nil
}

func (exporting exporter) lineDescription(line StatementLineView) string {
	description := exporting.label("source." + line.SourceType)
	if line.Memo != nil && strings.TrimSpace(*line.Memo) != "" {
		return description + " · " + strings.TrimSpace(*line.Memo)
	}
	counterpartNames := []string{}
	for _, counterpart := range line.Counterparts {
		counterpartNames = append(counterpartNames, exporting.accountName("", counterpart.SystemKey, counterpart.Name))
	}
	const shownCounterparts = 2
	if len(counterpartNames) > shownCounterparts {
		counterpartNames = append(counterpartNames[:shownCounterparts], "+"+strconv.Itoa(len(counterpartNames)-shownCounterparts))
	}
	if len(counterpartNames) == 0 {
		return description
	}
	return description + " · " + strings.Join(counterpartNames, ", ")
}

func zeroAsBlank(amount int64) any {
	if amount == 0 {
		return nil
	}
	return amount
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
