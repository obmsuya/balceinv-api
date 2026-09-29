package accounting

import (
	"context"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/google/uuid"
)

const (
	vatDueDay            = 20
	maximumStatementDays = 366
)

type ReportRequest struct {
	FromDate string
	ToDate   string
	Shop     string
}

type reportScope struct {
	books    Books
	period   periodFilter
	accounts []Account
}

func (service *Service) scope(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest, defaultFromDate func(toDate time.Time) time.Time) (reportScope, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return reportScope{}, booksError
	}

	toDate, toError := parseDateOr(request.ToDate, books.Today())
	if toError != nil {
		return reportScope{}, toError
	}
	fromDate, fromError := parseDateOr(request.FromDate, defaultFromDate(toDate).Format(dateLayout))
	if fromError != nil {
		return reportScope{}, fromError
	}
	if fromDate.After(toDate) {
		return reportScope{}, ErrInvalidDate
	}

	shopId, shopError := service.optionalShop(ctx, querier, principal.CompanyId, &request.Shop)
	if shopError != nil {
		return reportScope{}, shopError
	}
	accounts, accountsError := service.repository.ListAccounts(ctx, querier, principal.CompanyId)
	if accountsError != nil {
		return reportScope{}, accountsError
	}

	scope := reportScope{
		books:    books,
		period:   periodFilter{FromDate: fromDate.Format(dateLayout), ToDate: toDate.Format(dateLayout), ShopId: shopId},
		accounts: accounts,
	}
	return scope, nil
}

func parseDateOr(rawDate string, fallbackDate string) (time.Time, error) {
	if rawDate == "" {
		rawDate = fallbackDate
	}
	parsedDate, parseError := time.Parse(dateLayout, rawDate)
	if parseError != nil || !datePattern.MatchString(rawDate) {
		return time.Time{}, ErrInvalidDate
	}
	return parsedDate, nil
}

func firstOfMonth(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func firstOfYear(date time.Time) time.Time {
	return time.Date(date.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
}

func vatDueDate(month string) string {
	monthStart, parseError := time.Parse("2006-01", month[:7])
	if parseError != nil {
		return ""
	}
	return time.Date(monthStart.Year(), monthStart.Month()+1, vatDueDay, 0, 0, 0, 0, time.UTC).Format(dateLayout)
}

func naturalBalance(accountType string, totals accountTotals) int64 {
	if accountType == TypeAsset || accountType == TypeExpense {
		return totals.Debit - totals.Credit
	}
	return totals.Credit - totals.Debit
}

func amountView(account Account, amount int64) AccountAmountView {
	return AccountAmountView{
		AccountId: account.Id,
		Code:      account.Code,
		SystemKey: account.SystemKey,
		Name:      account.Name,
		Type:      account.Type,
		Amount:    amount,
	}
}

func hasKey(account Account, systemKey string) bool {
	return account.SystemKey != nil && *account.SystemKey == systemKey
}

func (service *Service) Overview(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest) (OverviewView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, request, firstOfMonth)
	if scopeError != nil {
		return OverviewView{}, scopeError
	}
	periodTotals, periodError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, scope.period)
	if periodError != nil {
		return OverviewView{}, periodError
	}
	sinceStartPeriod := periodFilter{ToDate: scope.period.ToDate, ShopId: scope.period.ShopId}
	cumulativeTotals, cumulativeError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, sinceStartPeriod)
	if cumulativeError != nil {
		return OverviewView{}, cumulativeError
	}
	moneyFlows, flowsError := service.repository.MoneyFlows(ctx, querier, principal.CompanyId, scope.period)
	if flowsError != nil {
		return OverviewView{}, flowsError
	}

	overview := OverviewView{FromDate: scope.period.FromDate, ToDate: scope.period.ToDate}
	for _, moneyFlow := range moneyFlows {
		if moneyFlow.SourceType == SourceOpening {
			continue
		}
		isReversal := moneyFlow.SourceType == SourceReversal
		switch {
		case moneyFlow.NetChange > 0 && isReversal:
			overview.MoneyOut -= moneyFlow.NetChange
		case moneyFlow.NetChange > 0:
			overview.MoneyIn += moneyFlow.NetChange
		case moneyFlow.NetChange < 0 && isReversal:
			overview.MoneyIn += moneyFlow.NetChange
		case moneyFlow.NetChange < 0:
			overview.MoneyOut -= moneyFlow.NetChange
		}
	}

	vatSummary := VatSummaryView{DueDate: vatDueDate(scope.period.ToDate)}
	for _, account := range scope.accounts {
		periodBalance := naturalBalance(account.Type, periodTotals[account.Id])
		cumulativeBalance := naturalBalance(account.Type, cumulativeTotals[account.Id])
		switch account.Type {
		case TypeIncome:
			overview.Income += periodBalance
		case TypeExpense:
			overview.Costs += periodBalance
		case TypeAsset:
			overview.WhatIOwn += cumulativeBalance
		case TypeLiability:
			overview.WhatIOwe += cumulativeBalance
		}
		switch {
		case hasKey(account, KeyCash):
			overview.Balances.Cash = cumulativeBalance
		case hasKey(account, KeyMobileMoney):
			overview.Balances.MobileMoney = cumulativeBalance
		case hasKey(account, KeyBank):
			overview.Balances.Bank = cumulativeBalance
		case hasKey(account, KeyCardClearing):
			overview.Balances.CardClearing = cumulativeBalance
		case hasKey(account, KeyVatOutput):
			vatSummary.Charged = periodBalance
		case hasKey(account, KeyVatInput):
			vatSummary.Reclaimable = periodBalance
		}
	}
	overview.Profit = overview.Income - overview.Costs
	if scope.books.VatRegistered {
		vatSummary.ToPay = vatSummary.Charged - vatSummary.Reclaimable
		overview.Vat = &vatSummary
	}
	return overview, nil
}

func (service *Service) ProfitAndLoss(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest) (ProfitAndLossView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, request, firstOfMonth)
	if scopeError != nil {
		return ProfitAndLossView{}, scopeError
	}
	periodTotals, periodError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, scope.period)
	if periodError != nil {
		return ProfitAndLossView{}, periodError
	}
	return profitAndLoss(scope.accounts, periodTotals, scope.period), nil
}

func profitAndLoss(accounts []Account, periodTotals map[uuid.UUID]accountTotals, period periodFilter) ProfitAndLossView {
	report := ProfitAndLossView{
		FromDate: period.FromDate,
		ToDate:   period.ToDate,
		Income:   []AccountAmountView{},
		Expenses: []AccountAmountView{},
	}
	for _, account := range accounts {
		accountTotal, hasActivity := periodTotals[account.Id]
		if !hasActivity {
			continue
		}
		amount := naturalBalance(account.Type, accountTotal)
		switch {
		case account.Type == TypeIncome:
			report.Income = append(report.Income, amountView(account, amount))
			report.TotalIncome += amount
		case hasKey(account, KeyCogs):
			report.CostOfGoods += amount
		case account.Type == TypeExpense:
			report.Expenses = append(report.Expenses, amountView(account, amount))
			report.TotalExpenses += amount
		}
	}
	report.GrossProfit = report.TotalIncome - report.CostOfGoods
	report.NetProfit = report.GrossProfit - report.TotalExpenses
	return report
}

func (service *Service) BalanceSheet(ctx context.Context, querier database.Querier, principal *identity.Principal, asOf string) (BalanceSheetView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, ReportRequest{ToDate: asOf}, firstOfMonth)
	if scopeError != nil {
		return BalanceSheetView{}, scopeError
	}
	cumulativeTotals, totalsError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, periodFilter{ToDate: scope.period.ToDate})
	if totalsError != nil {
		return BalanceSheetView{}, totalsError
	}

	balanceSheet := BalanceSheetView{
		AsOf:        scope.period.ToDate,
		Assets:      []AccountAmountView{},
		Liabilities: []AccountAmountView{},
		Equity:      []AccountAmountView{},
	}
	for _, account := range scope.accounts {
		accountTotal, hasActivity := cumulativeTotals[account.Id]
		if !hasActivity {
			continue
		}
		amount := naturalBalance(account.Type, accountTotal)
		switch account.Type {
		case TypeAsset:
			balanceSheet.Assets = append(balanceSheet.Assets, amountView(account, amount))
			balanceSheet.TotalAssets += amount
		case TypeLiability:
			balanceSheet.Liabilities = append(balanceSheet.Liabilities, amountView(account, amount))
			balanceSheet.TotalLiabilities += amount
		case TypeEquity:
			balanceSheet.Equity = append(balanceSheet.Equity, amountView(account, amount))
			balanceSheet.TotalEquity += amount
		case TypeIncome:
			balanceSheet.ProfitToDate += amount
		case TypeExpense:
			balanceSheet.ProfitToDate -= amount
		}
	}
	balanceSheet.TotalEquity += balanceSheet.ProfitToDate
	balanceSheet.IsBalanced = balanceSheet.TotalAssets == balanceSheet.TotalLiabilities+balanceSheet.TotalEquity
	return balanceSheet, nil
}

func (service *Service) TrialBalance(ctx context.Context, querier database.Querier, principal *identity.Principal, asOf string) (TrialBalanceView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, ReportRequest{ToDate: asOf}, firstOfMonth)
	if scopeError != nil {
		return TrialBalanceView{}, scopeError
	}
	cumulativeTotals, totalsError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, periodFilter{ToDate: scope.period.ToDate})
	if totalsError != nil {
		return TrialBalanceView{}, totalsError
	}

	trialBalance := TrialBalanceView{AsOf: scope.period.ToDate, Rows: []TrialBalanceRowView{}}
	for _, account := range scope.accounts {
		accountTotal, hasActivity := cumulativeTotals[account.Id]
		if !hasActivity {
			continue
		}
		netDebit := accountTotal.Debit - accountTotal.Credit
		row := TrialBalanceRowView{
			AccountAmountView: amountView(account, naturalBalance(account.Type, accountTotal)),
			TotalDebit:        accountTotal.Debit,
			TotalCredit:       accountTotal.Credit,
		}
		if netDebit >= 0 {
			row.DebitBalance = netDebit
		} else {
			row.CreditBalance = -netDebit
		}
		trialBalance.TotalDebitBalance += row.DebitBalance
		trialBalance.TotalCreditBalance += row.CreditBalance
		trialBalance.Rows = append(trialBalance.Rows, row)
	}
	trialBalance.IsBalanced = trialBalance.TotalDebitBalance == trialBalance.TotalCreditBalance
	return trialBalance, nil
}

func (service *Service) Statement(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest, rawAccount string) (StatementView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, request, firstOfMonth)
	if scopeError != nil {
		return StatementView{}, scopeError
	}
	fromDate, _ := time.Parse(dateLayout, scope.period.FromDate)
	toDate, _ := time.Parse(dateLayout, scope.period.ToDate)
	if fromDate.AddDate(0, 0, maximumStatementDays).Before(toDate.AddDate(0, 0, 1)) {
		return StatementView{}, ErrRangeTooLong
	}

	chosenAccount, isFound := Account{}, false
	parsedAccountId, parseError := uuid.Parse(rawAccount)
	for _, account := range scope.accounts {
		matchesId := parseError == nil && account.Id == parsedAccountId
		if matchesId || hasKey(account, rawAccount) {
			chosenAccount, isFound = account, true
		}
	}
	if !isFound {
		return StatementView{}, ErrAccountNotFound
	}

	beforePeriod := periodFilter{ToDate: fromDate.AddDate(0, 0, -1).Format(dateLayout), ShopId: scope.period.ShopId}
	openingTotals, openingError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, beforePeriod)
	if openingError != nil {
		return StatementView{}, openingError
	}
	statementLines, linesError := service.repository.StatementLines(ctx, querier, principal.CompanyId, chosenAccount.Id, scope.period)
	if linesError != nil {
		return StatementView{}, linesError
	}

	statement := StatementView{
		Account:        toAccountView(chosenAccount),
		FromDate:       scope.period.FromDate,
		ToDate:         scope.period.ToDate,
		OpeningBalance: naturalBalance(chosenAccount.Type, openingTotals[chosenAccount.Id]),
		Lines:          statementLines,
	}
	runningBalance := statement.OpeningBalance
	for lineIndex := range statement.Lines {
		statementLine := &statement.Lines[lineIndex]
		runningBalance += naturalBalance(chosenAccount.Type, accountTotals{Debit: statementLine.Debit, Credit: statementLine.Credit})
		statementLine.Balance = runningBalance
		statement.TotalDebit += statementLine.Debit
		statement.TotalCredit += statementLine.Credit
	}
	statement.ClosingBalance = runningBalance
	return statement, nil
}

func (service *Service) VatReport(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest) (VatReportView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, request, firstOfYear)
	if scopeError != nil {
		return VatReportView{}, scopeError
	}
	months, monthsError := service.repository.MonthlyVat(ctx, querier, principal.CompanyId, scope.period)
	if monthsError != nil {
		return VatReportView{}, monthsError
	}

	vatReport := VatReportView{FromDate: scope.period.FromDate, ToDate: scope.period.ToDate, Months: []VatMonthView{}}
	for _, month := range months {
		monthView := VatMonthView{
			Month:       month.Month,
			Charged:     month.Charged,
			Reclaimable: month.Reclaimable,
			ToPay:       month.Charged - month.Reclaimable,
			DueDate:     vatDueDate(month.Month),
		}
		vatReport.Months = append(vatReport.Months, monthView)
		vatReport.TotalCharged += monthView.Charged
		vatReport.TotalReclaimable += monthView.Reclaimable
		vatReport.TotalToPay += monthView.ToPay
	}
	return vatReport, nil
}

func (service *Service) Integrity(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReportRequest) (IntegrityView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, request, firstOfMonth)
	if scopeError != nil {
		return IntegrityView{}, scopeError
	}
	books := scope.books

	allTimeTotals, allTimeError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, periodFilter{ToDate: "9999-12-31"})
	if allTimeError != nil {
		return IntegrityView{}, allTimeError
	}
	periodTotals, periodError := service.repository.AccountTotals(ctx, querier, principal.CompanyId, periodFilter{FromDate: scope.period.FromDate, ToDate: scope.period.ToDate})
	if periodError != nil {
		return IntegrityView{}, periodError
	}

	integrity := IntegrityView{FromDate: scope.period.FromDate, ToDate: scope.period.ToDate, VatRegistered: books.VatRegistered}
	for _, account := range scope.accounts {
		integrity.TotalDebit += allTimeTotals[account.Id].Debit
		integrity.TotalCredit += allTimeTotals[account.Id].Credit
		if hasKey(account, KeySales) {
			integrity.LedgerSales = naturalBalance(account.Type, periodTotals[account.Id])
		}
		if hasKey(account, KeyInventory) {
			integrity.InventoryAccount = naturalBalance(account.Type, allTimeTotals[account.Id])
		}
	}
	integrity.IsBalanced = integrity.TotalDebit == integrity.TotalCredit

	fromDate, _ := time.ParseInLocation(dateLayout, scope.period.FromDate, books.Location)
	toDate, _ := time.ParseInLocation(dateLayout, scope.period.ToDate, books.Location)
	salesFrom := fromDate.UTC()
	if salesFrom.Before(books.StartedAt) {
		salesFrom = books.StartedAt
	}
	_, salesTotal, taxTotal, salesError := service.repository.SalesBetween(ctx, querier, principal.CompanyId, salesFrom, toDate.AddDate(0, 0, 1).UTC())
	if salesError != nil {
		return IntegrityView{}, salesError
	}
	integrity.SalesReportTotal = salesTotal
	integrity.SalesReportTax = taxTotal
	integrity.ExpectedLedgerSales = salesTotal
	if books.VatRegistered {
		integrity.ExpectedLedgerSales = salesTotal - taxTotal
	}
	integrity.SalesDifference = integrity.LedgerSales - integrity.ExpectedLedgerSales

	unpostedCount, countError := service.ledger.CountUnposted(ctx, querier, books)
	if countError != nil {
		return IntegrityView{}, countError
	}
	integrity.UnpostedCount = unpostedCount

	liveStockValue, stockError := service.repository.LiveStockValue(ctx, querier, principal.CompanyId)
	if stockError != nil {
		return IntegrityView{}, stockError
	}
	integrity.LiveStockValue = liveStockValue
	integrity.InventoryDifference = integrity.InventoryAccount - liveStockValue
	return integrity, nil
}
