package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const earliestDate = "0001-01-01"

type accountTotals struct {
	Debit  int64
	Credit int64
}

type periodFilter struct {
	FromDate string
	ToDate   string
	ShopId   *uuid.UUID
}

func linesWhere(companyId uuid.UUID, period periodFilter, arguments *queryArguments) string {
	fromDate := period.FromDate
	if fromDate == "" {
		fromDate = earliestDate
	}
	where := `l.company_id = ` + arguments.add(companyId) +
		` AND e.entry_date >= ` + arguments.add(fromDate) +
		` AND e.entry_date <= ` + arguments.add(period.ToDate)
	if period.ShopId != nil {
		where += ` AND l.shop_id = ` + arguments.add(*period.ShopId)
	}
	return where
}

const linesJoin = `
	FROM journal_lines l
	JOIN journal_entries e ON e.company_id = l.company_id AND e.id = l.entry_id
	JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
`

func (repository *Repository) AccountTotals(ctx context.Context, querier database.Querier, companyId uuid.UUID, period periodFilter) (map[uuid.UUID]accountTotals, error) {
	arguments := &queryArguments{}
	query := `
		SELECT l.account_id, CAST(COALESCE(SUM(l.debit), 0) AS BIGINT), CAST(COALESCE(SUM(l.credit), 0) AS BIGINT)
	` + linesJoin + ` WHERE ` + linesWhere(companyId, period, arguments) + ` GROUP BY l.account_id`

	totalRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total the accounts: %w", queryError)
	}
	defer totalRows.Close()

	totalsByAccount := map[uuid.UUID]accountTotals{}
	for totalRows.Next() {
		accountId := uuid.UUID{}
		totals := accountTotals{}
		scanError := totalRows.Scan(&accountId, &totals.Debit, &totals.Credit)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan account totals: %w", scanError)
		}
		totalsByAccount[accountId] = totals
	}
	return totalsByAccount, totalRows.Err()
}

type entryMoneyFlow struct {
	SourceType string
	NetChange  int64
}

func (repository *Repository) MoneyFlows(ctx context.Context, querier database.Querier, companyId uuid.UUID, period periodFilter) ([]entryMoneyFlow, error) {
	arguments := &queryArguments{}
	query := `
		SELECT e.source_type, CAST(SUM(l.debit - l.credit) AS BIGINT)
	` + linesJoin + ` WHERE ` + linesWhere(companyId, period, arguments) +
		` AND a.system_key IN (` + joinedPlaceholders(arguments, moneyKeys) + `)
		GROUP BY e.id, e.source_type`

	flowRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total money in and out: %w", queryError)
	}
	defer flowRows.Close()

	moneyFlows := []entryMoneyFlow{}
	for flowRows.Next() {
		moneyFlow := entryMoneyFlow{}
		scanError := flowRows.Scan(&moneyFlow.SourceType, &moneyFlow.NetChange)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a money flow: %w", scanError)
		}
		moneyFlows = append(moneyFlows, moneyFlow)
	}
	return moneyFlows, flowRows.Err()
}

type monthlyVat struct {
	Month       string
	Charged     int64
	Reclaimable int64
}

func (repository *Repository) MonthlyVat(ctx context.Context, querier database.Querier, companyId uuid.UUID, period periodFilter) ([]monthlyVat, error) {
	arguments := &queryArguments{}
	query := `
		SELECT substr(e.entry_date, 1, 7),
		       CAST(COALESCE(SUM(CASE WHEN a.system_key = 'vat_output' THEN l.credit - l.debit ELSE 0 END), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN a.system_key = 'vat_input' THEN l.debit - l.credit ELSE 0 END), 0) AS BIGINT)
	` + linesJoin + ` WHERE ` + linesWhere(companyId, period, arguments) + `
		  AND a.system_key IN ('vat_output', 'vat_input')
		GROUP BY substr(e.entry_date, 1, 7)
		ORDER BY substr(e.entry_date, 1, 7)
	`

	vatRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total VAT by month: %w", queryError)
	}
	defer vatRows.Close()

	months := []monthlyVat{}
	for vatRows.Next() {
		month := monthlyVat{}
		scanError := vatRows.Scan(&month.Month, &month.Charged, &month.Reclaimable)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a VAT month: %w", scanError)
		}
		months = append(months, month)
	}
	return months, vatRows.Err()
}

func (repository *Repository) StatementLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, accountId uuid.UUID, period periodFilter) ([]StatementLineView, error) {
	arguments := &queryArguments{}
	query := `
		SELECT e.id, e.entry_number, e.entry_date, e.source_type, e.memo, l.debit, l.credit, l.shop_id, sh.name
	` + linesJoin + `
		LEFT JOIN shops sh ON sh.company_id = l.company_id AND sh.id = l.shop_id
		WHERE ` + linesWhere(companyId, period, arguments) + ` AND l.account_id = ` + arguments.add(accountId) + `
		ORDER BY e.entry_date, e.entry_number, l.line_no
	`

	lineRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the account statement: %w", queryError)
	}
	defer lineRows.Close()

	statementLines := []StatementLineView{}
	for lineRows.Next() {
		statementLine := StatementLineView{}
		scanError := lineRows.Scan(&statementLine.EntryId, &statementLine.EntryNumber, &statementLine.EntryDate, &statementLine.SourceType,
			&statementLine.Memo, &statementLine.Debit, &statementLine.Credit, &statementLine.ShopId, &statementLine.ShopName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a statement line: %w", scanError)
		}
		statementLine.Number = formatEntryNumber(statementLine.EntryNumber)
		statementLines = append(statementLines, statementLine)
	}
	return statementLines, lineRows.Err()
}

func (repository *Repository) SalesBetween(ctx context.Context, querier database.Querier, companyId uuid.UUID, from time.Time, to time.Time) (int64, int64, int64, error) {
	query := `
		SELECT COUNT(*), CAST(COALESCE(SUM(total), 0) AS BIGINT), CAST(COALESCE(SUM(tax_total), 0) AS BIGINT)
		FROM sales
		WHERE company_id = $1 AND created_at >= $2 AND created_at < $3
	`
	saleCount := int64(0)
	salesTotal := int64(0)
	taxTotal := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, from, to).Scan(&saleCount, &salesTotal, &taxTotal)
	if scanError != nil {
		return 0, 0, 0, fmt.Errorf("failed to total the sales: %w", scanError)
	}
	return saleCount, salesTotal, taxTotal, nil
}

func (repository *Repository) LiveStockValue(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `
		SELECT CAST(COALESCE((SELECT SUM(ss.quantity * p.cost_price) FROM shop_stock ss
		                      JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id
		                      WHERE ss.company_id = $1), 0) AS BIGINT)
		     + CAST(COALESCE((SELECT SUM(ol.quantity * p.cost_price) FROM customer_orders o
		                      JOIN customer_order_lines ol ON ol.company_id = o.company_id AND ol.order_id = o.id
		                      JOIN products p ON p.company_id = ol.company_id AND p.id = ol.product_id
		                      WHERE o.company_id = $1 AND o.status IN ('open', 'ready')), 0) AS BIGINT)
	`

	stockValue := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&stockValue)
	if scanError != nil {
		return 0, fmt.Errorf("failed to value the stock: %w", scanError)
	}
	return stockValue, nil
}

type EntryFilter struct {
	FromDate    string
	ToDate      string
	SourceTypes []string
	AccountId   *uuid.UUID
	ShopId      *uuid.UUID
}

func entriesWhere(companyId uuid.UUID, filter EntryFilter, arguments *queryArguments) string {
	conditions := []string{`e.company_id = ` + arguments.add(companyId)}
	if filter.FromDate != "" {
		conditions = append(conditions, `e.entry_date >= `+arguments.add(filter.FromDate))
	}
	if filter.ToDate != "" {
		conditions = append(conditions, `e.entry_date <= `+arguments.add(filter.ToDate))
	}
	if len(filter.SourceTypes) > 0 {
		conditions = append(conditions, `e.source_type IN (`+joinedPlaceholders(arguments, filter.SourceTypes)+`)`)
	}
	if filter.AccountId != nil {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM journal_lines fl WHERE fl.company_id = e.company_id AND fl.entry_id = e.id AND fl.account_id = `+arguments.add(*filter.AccountId)+`)`)
	}
	if filter.ShopId != nil {
		shopPlaceholder := arguments.add(*filter.ShopId)
		conditions = append(conditions, `(e.shop_id = `+shopPlaceholder+` OR EXISTS (SELECT 1 FROM journal_lines sl WHERE sl.company_id = e.company_id AND sl.entry_id = e.id AND sl.shop_id = `+shopPlaceholder+`))`)
	}
	return strings.Join(conditions, " AND ")
}

func (repository *Repository) CountEntries(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter EntryFilter) (int64, error) {
	arguments := &queryArguments{}
	query := `SELECT COUNT(*) FROM journal_entries e WHERE ` + entriesWhere(companyId, filter, arguments)
	entryCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, arguments.values...).Scan(&entryCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count entries: %w", scanError)
	}
	return entryCount, nil
}

const entryViewSelect = `
	SELECT e.id, e.entry_number, e.entry_date, e.source_type, e.source_id, e.memo, e.shop_id, sh.name,
	       e.attachment_key, e.receipt_number, e.supplier_tin, e.party_type, e.party_id, e.reverses_entry_id,
	       (SELECT r.id FROM journal_entries r WHERE r.company_id = e.company_id AND r.reverses_entry_id = e.id LIMIT 1),
	       u.name, e.created_at,
	       CAST(COALESCE((SELECT SUM(l.debit) FROM journal_lines l WHERE l.company_id = e.company_id AND l.entry_id = e.id), 0) AS BIGINT)
	FROM journal_entries e
	LEFT JOIN shops sh ON sh.company_id = e.company_id AND sh.id = e.shop_id
	LEFT JOIN users u ON u.company_id = e.company_id AND u.id = e.created_by
`

func (repository *Repository) ListEntries(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter EntryFilter, limit int, offset int) ([]EntryView, error) {
	arguments := &queryArguments{}
	query := entryViewSelect + ` WHERE ` + entriesWhere(companyId, filter, arguments) +
		` ORDER BY e.entry_number DESC LIMIT ` + arguments.add(limit) + ` OFFSET ` + arguments.add(offset)
	return repository.queryEntries(ctx, querier, companyId, query, arguments.values...)
}

func (repository *Repository) FindEntryView(ctx context.Context, querier database.Querier, companyId uuid.UUID, entryId uuid.UUID) (*EntryView, error) {
	entryViews, queryError := repository.queryEntries(ctx, querier, companyId, entryViewSelect+` WHERE e.company_id = $1 AND e.id = $2`, companyId, entryId)
	if queryError != nil {
		return nil, queryError
	}
	if len(entryViews) == 0 {
		return nil, nil
	}
	return &entryViews[0], nil
}

func (repository *Repository) FindAttachmentKey(ctx context.Context, querier database.Querier, companyId uuid.UUID, entryId uuid.UUID) (*string, error) {
	attachmentKey := (*string)(nil)
	scanError := querier.QueryRowContext(ctx, `SELECT attachment_key FROM journal_entries WHERE company_id = $1 AND id = $2`, companyId, entryId).Scan(&attachmentKey)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the receipt photo: %w", scanError)
	}
	return attachmentKey, nil
}

func (repository *Repository) queryEntries(ctx context.Context, querier database.Querier, companyId uuid.UUID, query string, arguments ...any) ([]EntryView, error) {
	entryRows, queryError := querier.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list entries: %w", queryError)
	}
	defer entryRows.Close()

	entryViews := []EntryView{}
	indexByEntry := map[uuid.UUID]int{}
	for entryRows.Next() {
		entryView := EntryView{Lines: []LineView{}}
		attachmentKey := (*string)(nil)
		scanError := entryRows.Scan(&entryView.Id, &entryView.EntryNumber, &entryView.EntryDate, &entryView.SourceType, &entryView.SourceId,
			&entryView.Memo, &entryView.ShopId, &entryView.ShopName, &attachmentKey, &entryView.ReceiptNumber, &entryView.SupplierTin,
			&entryView.PartyType, &entryView.PartyId, &entryView.ReversesEntryId, &entryView.ReversedByEntryId, &entryView.CreatedByName,
			&entryView.CreatedAt, &entryView.Amount)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an entry: %w", scanError)
		}
		entryView.Number = formatEntryNumber(entryView.EntryNumber)
		entryView.HasAttachment = attachmentKey != nil
		entryView.IsReversible = reversibleSources[entryView.SourceType] && entryView.ReversedByEntryId == nil
		indexByEntry[entryView.Id] = len(entryViews)
		entryViews = append(entryViews, entryView)
	}
	rowsError := entryRows.Err()
	if rowsError != nil {
		return nil, rowsError
	}
	if len(entryViews) == 0 {
		return entryViews, nil
	}

	lineArguments := &queryArguments{}
	entryPlaceholders := []string{}
	companyPlaceholder := lineArguments.add(companyId)
	for _, entryView := range entryViews {
		entryPlaceholders = append(entryPlaceholders, lineArguments.add(entryView.Id))
	}
	lineQuery := `
		SELECT l.entry_id, l.line_no, l.account_id, a.code, a.system_key, a.name, a.type, l.debit, l.credit, l.shop_id, sh.name
		FROM journal_lines l
		JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
		LEFT JOIN shops sh ON sh.company_id = l.company_id AND sh.id = l.shop_id
		WHERE l.company_id = ` + companyPlaceholder + ` AND l.entry_id IN (` + strings.Join(entryPlaceholders, ", ") + `)
		ORDER BY l.entry_id, l.line_no
	`
	lineRows, lineQueryError := querier.QueryContext(ctx, lineQuery, lineArguments.values...)
	if lineQueryError != nil {
		return nil, fmt.Errorf("failed to list entry lines: %w", lineQueryError)
	}
	defer lineRows.Close()

	for lineRows.Next() {
		entryId := uuid.UUID{}
		lineView := LineView{}
		scanError := lineRows.Scan(&entryId, &lineView.LineNo, &lineView.AccountId, &lineView.AccountCode, &lineView.AccountKey, &lineView.AccountName,
			&lineView.AccountType, &lineView.Debit, &lineView.Credit, &lineView.ShopId, &lineView.ShopName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an entry line: %w", scanError)
		}
		entryIndex := indexByEntry[entryId]
		entryViews[entryIndex].Lines = append(entryViews[entryIndex].Lines, lineView)
	}
	return entryViews, lineRows.Err()
}

func formatEntryNumber(entryNumber int64) string {
	return fmt.Sprintf("JE-%06d", entryNumber)
}
