package accounting

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const depositMethod = "deposit"

type partyOpening struct {
	PartyType      string
	PartyId        uuid.UUID
	OpeningBalance int64
	PostedBalance  int64
	CreatedAt      time.Time
}

func notPosted(alias string, sourceType string) string {
	return ` AND NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = ` + alias + `.company_id AND e.source_type = '` + sourceType + `' AND e.source_id = ` + alias + `.id)`
}

func originalPostedOrPending(alias string, sourceType string, createdColumn string, sincePlaceholder string) string {
	return ` AND (` + alias + `.` + createdColumn + ` >= ` + sincePlaceholder +
		` OR EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = ` + alias + `.company_id AND e.source_type = '` + sourceType + `' AND e.source_id = ` + alias + `.id))`
}

func onlyId(where string, alias string, arguments *queryArguments, documentId *uuid.UUID) string {
	if documentId == nil {
		return where
	}
	return where + ` AND ` + alias + `.id = ` + arguments.add(*documentId)
}

func scanAll[Posting any](rows *sql.Rows, rowsError error, label string, scanRow func(rows *sql.Rows) (Posting, error)) ([]Posting, error) {
	if rowsError != nil {
		return nil, fmt.Errorf("failed to list %s for the books: %w", label, rowsError)
	}
	defer rows.Close()

	postings := []Posting{}
	for rows.Next() {
		posting, scanError := scanRow(rows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan %s for the books: %w", label, scanError)
		}
		postings = append(postings, posting)
	}
	return postings, rows.Err()
}

func (repository *Repository) UnpostedPurchases(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPurchaseId *uuid.UUID) ([]PurchasePosting, error) {
	arguments := &queryArguments{}
	where := `p.company_id = ` + arguments.add(companyId) + ` AND p.created_at >= ` + arguments.add(since) + notPosted("p", SourcePurchase)
	query := `
		SELECT p.id, p.shop_id, p.supplier_id, p.purchase_number, p.subtotal, p.vat_total, p.received_at, p.created_by
		FROM purchases p WHERE ` + onlyId(where, "p", arguments, onlyPurchaseId) + ` ORDER BY p.created_at, p.id`
	purchaseRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(purchaseRows, queryError, "purchases", func(rows *sql.Rows) (PurchasePosting, error) {
		purchasePosting := PurchasePosting{CompanyId: companyId, PaidByMethod: map[string]int64{}}
		scanError := rows.Scan(&purchasePosting.PurchaseId, &purchasePosting.ShopId, &purchasePosting.SupplierId, &purchasePosting.Reference,
			&purchasePosting.NetCost, &purchasePosting.Vat, &purchasePosting.ReceivedAt, &purchasePosting.UserId)
		purchasePosting.OwedToSupplier = purchasePosting.NetCost + purchasePosting.Vat
		return purchasePosting, scanError
	})
}

func (repository *Repository) UnpostedPurchaseCancels(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPurchaseId *uuid.UUID) ([]ReversalPosting, error) {
	arguments := &queryArguments{}
	sincePlaceholder := arguments.add(since)
	where := `p.company_id = ` + arguments.add(companyId) + ` AND p.status = 'cancelled' AND p.cancelled_at >= ` + sincePlaceholder +
		notPosted("p", SourcePurchaseCancel) + originalPostedOrPending("p", SourcePurchase, "created_at", sincePlaceholder)
	query := `SELECT p.id, p.cancelled_at, p.cancel_reason, p.cancelled_by FROM purchases p WHERE ` + onlyId(where, "p", arguments, onlyPurchaseId)
	cancelRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(cancelRows, queryError, "cancelled purchases", func(rows *sql.Rows) (ReversalPosting, error) {
		reversal := ReversalPosting{CompanyId: companyId, OriginalSourceType: SourcePurchase, SourceType: SourcePurchaseCancel}
		reason := sql.NullString{}
		scanError := rows.Scan(&reversal.OriginalSourceId, &reversal.ReversedAt, &reason, &reversal.UserId)
		reversal.Reason = reason.String
		return reversal, scanError
	})
}

func (repository *Repository) UnpostedSupplierPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPaymentId *uuid.UUID) ([]PaymentPosting, error) {
	arguments := &queryArguments{}
	where := `sp.company_id = ` + arguments.add(companyId) + ` AND sp.created_at >= ` + arguments.add(since) + notPosted("sp", SourceSupplierPayment)
	query := `
		SELECT sp.id, sp.supplier_id, sp.shop_id, sp.paid_at, sp.method, sp.amount, sp.payment_number, sp.created_by
		FROM supplier_payments sp WHERE ` + onlyId(where, "sp", arguments, onlyPaymentId) + ` ORDER BY sp.created_at, sp.id`
	paymentRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(paymentRows, queryError, "supplier payments", func(rows *sql.Rows) (PaymentPosting, error) {
		paymentPosting := PaymentPosting{CompanyId: companyId}
		scanError := rows.Scan(&paymentPosting.SourceId, &paymentPosting.PartyId, &paymentPosting.ShopId, &paymentPosting.PaidAt,
			&paymentPosting.Method, &paymentPosting.Amount, &paymentPosting.Reference, &paymentPosting.UserId)
		return paymentPosting, scanError
	})
}

func (repository *Repository) UnpostedSupplierPaymentVoids(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPaymentId *uuid.UUID) ([]ReversalPosting, error) {
	arguments := &queryArguments{}
	sincePlaceholder := arguments.add(since)
	where := `sp.company_id = ` + arguments.add(companyId) + ` AND sp.voided_at IS NOT NULL AND sp.voided_at >= ` + sincePlaceholder +
		notPosted("sp", SourceSupplierPaymentVoid) + originalPostedOrPending("sp", SourceSupplierPayment, "created_at", sincePlaceholder)
	query := `SELECT sp.id, sp.voided_at, sp.void_reason, sp.voided_by FROM supplier_payments sp WHERE ` + onlyId(where, "sp", arguments, onlyPaymentId)
	voidRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(voidRows, queryError, "voided supplier payments", func(rows *sql.Rows) (ReversalPosting, error) {
		reversal := ReversalPosting{CompanyId: companyId, OriginalSourceType: SourceSupplierPayment, SourceType: SourceSupplierPaymentVoid}
		reason := sql.NullString{}
		scanError := rows.Scan(&reversal.OriginalSourceId, &reversal.ReversedAt, &reason, &reversal.UserId)
		reversal.Reason = reason.String
		return reversal, scanError
	})
}

func (repository *Repository) UnpostedSupplierReturns(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyReturnId *uuid.UUID) ([]SupplierReturnPosting, error) {
	arguments := &queryArguments{}
	where := `sr.company_id = ` + arguments.add(companyId) + ` AND sr.created_at >= ` + arguments.add(since) + notPosted("sr", SourceSupplierReturn)
	query := `
		SELECT sr.id, sr.supplier_id, sr.shop_id, sr.returned_at, sr.total, sr.return_number, sr.created_by
		FROM supplier_returns sr WHERE ` + onlyId(where, "sr", arguments, onlyReturnId) + ` ORDER BY sr.created_at, sr.id`
	returnRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(returnRows, queryError, "supplier returns", func(rows *sql.Rows) (SupplierReturnPosting, error) {
		returnPosting := SupplierReturnPosting{CompanyId: companyId, RefundByMethod: map[string]int64{}}
		supplierId := uuid.UUID{}
		scanError := rows.Scan(&returnPosting.ReturnId, &supplierId, &returnPosting.ShopId, &returnPosting.ReturnedAt,
			&returnPosting.NetCost, &returnPosting.Reference, &returnPosting.UserId)
		returnPosting.SupplierId = &supplierId
		returnPosting.CreditFromSupplier = returnPosting.NetCost
		return returnPosting, scanError
	})
}

func (repository *Repository) UnpostedCustomerPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPaymentId *uuid.UUID) ([]PaymentPosting, error) {
	arguments := &queryArguments{}
	where := `cp.company_id = ` + arguments.add(companyId) + ` AND cp.received_at >= ` + arguments.add(since) + notPosted("cp", SourceCustomerPayment)
	query := `
		SELECT cp.id, cp.customer_id, cp.shop_id, cp.received_at, cp.method, cp.amount, cp.reference, cp.created_by
		FROM customer_payments cp WHERE ` + onlyId(where, "cp", arguments, onlyPaymentId) + ` ORDER BY cp.received_at, cp.id`
	paymentRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(paymentRows, queryError, "customer payments", func(rows *sql.Rows) (PaymentPosting, error) {
		paymentPosting := PaymentPosting{CompanyId: companyId}
		reference := sql.NullString{}
		scanError := rows.Scan(&paymentPosting.SourceId, &paymentPosting.PartyId, &paymentPosting.ShopId, &paymentPosting.PaidAt,
			&paymentPosting.Method, &paymentPosting.Amount, &reference, &paymentPosting.UserId)
		paymentPosting.Reference = reference.String
		return paymentPosting, scanError
	})
}

func (repository *Repository) UnpostedCustomerPaymentVoids(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPaymentId *uuid.UUID) ([]ReversalPosting, error) {
	arguments := &queryArguments{}
	sincePlaceholder := arguments.add(since)
	where := `cp.company_id = ` + arguments.add(companyId) + ` AND cp.voided_at IS NOT NULL AND cp.voided_at >= ` + sincePlaceholder +
		notPosted("cp", SourceCustomerPaymentVoid) + originalPostedOrPending("cp", SourceCustomerPayment, "received_at", sincePlaceholder)
	query := `SELECT cp.id, cp.voided_at, cp.void_reason, cp.voided_by FROM customer_payments cp WHERE ` + onlyId(where, "cp", arguments, onlyPaymentId)
	voidRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(voidRows, queryError, "voided customer payments", func(rows *sql.Rows) (ReversalPosting, error) {
		reversal := ReversalPosting{CompanyId: companyId, OriginalSourceType: SourceCustomerPayment, SourceType: SourceCustomerPaymentVoid}
		reason := sql.NullString{}
		scanError := rows.Scan(&reversal.OriginalSourceId, &reversal.ReversedAt, &reason, &reversal.UserId)
		reversal.Reason = reason.String
		return reversal, scanError
	})
}

type orderMoneyPosting struct {
	Kind    string
	Payment PaymentPosting
}

func (repository *Repository) UnpostedOrderMoney(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyPaymentId *uuid.UUID) ([]orderMoneyPosting, error) {
	arguments := &queryArguments{}
	where := `op.company_id = ` + arguments.add(companyId) + ` AND op.created_at >= ` + arguments.add(since) +
		` AND NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = op.company_id AND e.source_type IN ('order_deposit', 'order_refund') AND e.source_id = op.id)`
	query := `
		SELECT op.id, op.kind, o.customer_id, o.shop_id, op.created_at, op.method, op.amount, o.number, op.created_by
		FROM customer_order_payments op
		JOIN customer_orders o ON o.company_id = op.company_id AND o.id = op.order_id
		WHERE ` + onlyId(where, "op", arguments, onlyPaymentId) + ` ORDER BY op.created_at, op.id`
	moneyRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(moneyRows, queryError, "order deposits", func(rows *sql.Rows) (orderMoneyPosting, error) {
		orderMoney := orderMoneyPosting{Payment: PaymentPosting{CompanyId: companyId}}
		customerId := uuid.UUID{}
		shopId := uuid.UUID{}
		orderNumber := int64(0)
		scanError := rows.Scan(&orderMoney.Payment.SourceId, &orderMoney.Kind, &customerId, &shopId, &orderMoney.Payment.PaidAt,
			&orderMoney.Payment.Method, &orderMoney.Payment.Amount, &orderNumber, &orderMoney.Payment.UserId)
		orderMoney.Payment.PartyId = &customerId
		orderMoney.Payment.ShopId = &shopId
		orderMoney.Payment.Reference = fmt.Sprintf("ORD-%06d", orderNumber)
		return orderMoney, scanError
	})
}

func (repository *Repository) UnpostedPartyOpenings(ctx context.Context, querier database.Querier, companyId uuid.UUID, partyType string, onlyPartyId *uuid.UUID) ([]partyOpening, error) {
	partyTable, controlKey, postedSign := "customers", KeyReceivable, "l.debit - l.credit"
	if partyType == PartySupplier {
		partyTable, controlKey, postedSign = "suppliers", KeyPayable, "l.credit - l.debit"
	}
	arguments := &queryArguments{}
	postedQuery := `CAST(COALESCE((SELECT SUM(` + postedSign + `) FROM journal_lines l
		JOIN journal_entries e ON e.company_id = l.company_id AND e.id = l.entry_id
		JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
		WHERE e.company_id = party.company_id AND e.source_type = 'opening' AND e.party_type = ` + arguments.add(partyType) + `
		  AND e.party_id = party.id AND a.system_key = ` + arguments.add(controlKey) + `), 0) AS BIGINT)`
	where := `party.company_id = ` + arguments.add(companyId)
	query := `
		SELECT party.id, party.opening_balance, ` + postedQuery + `, party.created_at
		FROM ` + partyTable + ` party
		WHERE ` + onlyId(where, "party", arguments, onlyPartyId) + ` AND party.opening_balance <> ` + postedQuery
	openingRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(openingRows, queryError, "opening balances", func(rows *sql.Rows) (partyOpening, error) {
		opening := partyOpening{PartyType: partyType}
		scanError := rows.Scan(&opening.PartyId, &opening.OpeningBalance, &opening.PostedBalance, &opening.CreatedAt)
		return opening, scanError
	})
}

func (repository *Repository) ReservedStockValueByShopAt(ctx context.Context, querier database.Querier, companyId uuid.UUID, at time.Time) (map[uuid.UUID]int64, error) {
	query := `
		SELECT o.shop_id, CAST(COALESCE(SUM(ol.quantity * p.cost_price), 0) AS BIGINT)
		FROM customer_orders o
		JOIN customer_order_lines ol ON ol.company_id = o.company_id AND ol.order_id = o.id
		JOIN products p ON p.company_id = ol.company_id AND p.id = ol.product_id
		WHERE o.company_id = $1 AND o.created_at < $2
		  AND (o.status IN ('open', 'ready') OR o.collected_at >= $2 OR o.cancelled_at >= $2)
		GROUP BY o.shop_id
	`
	valueRows, queryError := querier.QueryContext(ctx, query, companyId, at)
	shopValues, scanError := scanAll(valueRows, queryError, "reserved stock", func(rows *sql.Rows) (shopValue, error) {
		value := shopValue{}
		rowError := rows.Scan(&value.ShopId, &value.Value)
		return value, rowError
	})
	if scanError != nil {
		return nil, scanError
	}
	valueByShop := map[uuid.UUID]int64{}
	for _, value := range shopValues {
		valueByShop[value.ShopId] = value.Value
	}
	return valueByShop, nil
}
