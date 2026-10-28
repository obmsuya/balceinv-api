package accounting

import (
	"context"
	"database/sql"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

func (ledger *Ledger) PostSupplierReceiptById(ctx context.Context, querier database.Querier, companyId uuid.UUID, receiptId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, receiptId, ledger.pendingSupplierReceipts)
}

func (ledger *Ledger) PostSupplierReceiptVoidById(ctx context.Context, querier database.Querier, companyId uuid.UUID, receiptId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, receiptId, ledger.pendingSupplierReceiptVoids)
}

func (ledger *Ledger) pendingSupplierReceipts(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	receiptPostings, loadError := ledger.repository.UnpostedSupplierReceipts(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(receiptPostings, loadError, func(posting PaymentPosting) pendingEvent {
		return pendingEvent{posting.PaidAt, true, func() (*PostedEntry, error) {
			return ledger.postPaymentWithBooks(ctx, querier, books, posting, SourceSupplierReceipt)
		}}
	})
}

func (ledger *Ledger) pendingSupplierReceiptVoids(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	reversals, loadError := ledger.repository.UnpostedSupplierReceiptVoids(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return ledger.reversalEvents(ctx, querier, books, reversals, loadError)
}

func (repository *Repository) UnpostedSupplierReceipts(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyReceiptId *uuid.UUID) ([]PaymentPosting, error) {
	arguments := &queryArguments{}
	where := `sr.company_id = ` + arguments.add(companyId) + ` AND sr.received_at >= ` + arguments.add(since) + notPosted("sr", SourceSupplierReceipt)
	query := `
		SELECT sr.id, sr.supplier_id, sr.shop_id, sr.received_at, sr.method, sr.amount, sr.reference, sr.created_by
		FROM supplier_receipts sr WHERE ` + onlyId(where, "sr", arguments, onlyReceiptId) + ` ORDER BY sr.received_at, sr.id`
	receiptRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(receiptRows, queryError, "money from suppliers", func(rows *sql.Rows) (PaymentPosting, error) {
		receiptPosting := PaymentPosting{CompanyId: companyId}
		reference := sql.NullString{}
		scanError := rows.Scan(&receiptPosting.SourceId, &receiptPosting.PartyId, &receiptPosting.ShopId, &receiptPosting.PaidAt,
			&receiptPosting.Method, &receiptPosting.Amount, &reference, &receiptPosting.UserId)
		receiptPosting.Reference = reference.String
		return receiptPosting, scanError
	})
}

func (repository *Repository) UnpostedSupplierReceiptVoids(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyReceiptId *uuid.UUID) ([]ReversalPosting, error) {
	arguments := &queryArguments{}
	sincePlaceholder := arguments.add(since)
	where := `sr.company_id = ` + arguments.add(companyId) + ` AND sr.voided_at IS NOT NULL AND sr.voided_at >= ` + sincePlaceholder +
		notPosted("sr", SourceSupplierReceiptVoid) + originalPostedOrPending("sr", SourceSupplierReceipt, "received_at", sincePlaceholder)
	query := `SELECT sr.id, sr.voided_at, sr.void_reason, sr.voided_by FROM supplier_receipts sr WHERE ` + onlyId(where, "sr", arguments, onlyReceiptId)
	voidRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(voidRows, queryError, "cancelled money from suppliers", func(rows *sql.Rows) (ReversalPosting, error) {
		reversal := ReversalPosting{CompanyId: companyId, OriginalSourceType: SourceSupplierReceipt, SourceType: SourceSupplierReceiptVoid}
		reason := sql.NullString{}
		scanError := rows.Scan(&reversal.OriginalSourceId, &reversal.ReversedAt, &reason, &reversal.UserId)
		reversal.Reason = reason.String
		return reversal, scanError
	})
}
