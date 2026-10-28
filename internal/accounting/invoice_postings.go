package accounting

import (
	"context"
	"database/sql"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type InvoicePosting struct {
	CompanyId   uuid.UUID
	InvoiceId   uuid.UUID
	ShopId      uuid.UUID
	DeliveredAt time.Time
	Number      string
	Total       int64
	TaxTotal    int64
	CostTotal   int64
	PartyType   string
	PartyId     uuid.UUID
	UserId      *uuid.UUID
}

func (ledger *Ledger) PostInvoiceById(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, invoiceId, ledger.pendingInvoices)
}

func (ledger *Ledger) PostInvoiceCancelById(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, invoiceId, ledger.pendingInvoiceCancels)
}

func (ledger *Ledger) postInvoice(ctx context.Context, querier database.Querier, books Books, posting InvoicePosting) (*PostedEntry, error) {
	shopId := posting.ShopId
	counterKey := KeyReceivable
	if posting.PartyType == PartySupplier {
		counterKey = KeyPayable
	}
	vatCharged := int64(0)
	if books.VatRegistered {
		vatCharged = posting.TaxTotal
	}
	invoiceLines := []keyedLine{
		{Key: counterKey, Debit: posting.Total, ShopId: &shopId},
		{Key: KeySales, Credit: posting.Total - vatCharged, ShopId: &shopId},
		{Key: KeyVatOutput, Credit: vatCharged, ShopId: &shopId},
		{Key: KeyCogs, Debit: posting.CostTotal, ShopId: &shopId},
		{Key: KeyInventory, Credit: posting.CostTotal, ShopId: &shopId},
	}

	partyType, partyId := partyOf(posting.PartyType, &posting.PartyId)
	header := Entry{
		SourceType: SourceInvoice,
		SourceId:   &posting.InvoiceId,
		Memo:       textOrNil(posting.Number),
		ShopId:     &shopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.DeliveredAt, header, invoiceLines)
}

func (ledger *Ledger) pendingInvoices(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	invoicePostings, loadError := ledger.repository.UnpostedInvoices(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(invoicePostings, loadError, func(posting InvoicePosting) pendingEvent {
		return pendingEvent{posting.DeliveredAt, posting.Total > 0 || posting.CostTotal > 0, func() (*PostedEntry, error) {
			return ledger.postInvoice(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingInvoiceCancels(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	reversals, loadError := ledger.repository.UnpostedInvoiceCancels(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return ledger.reversalEvents(ctx, querier, books, reversals, loadError)
}

func (repository *Repository) UnpostedInvoices(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyInvoiceId *uuid.UUID) ([]InvoicePosting, error) {
	arguments := &queryArguments{}
	where := `i.company_id = ` + arguments.add(companyId) + ` AND i.delivered_at IS NOT NULL AND i.delivered_at >= ` + arguments.add(since) + notPosted("i", SourceInvoice)
	query := `
		SELECT i.id, i.shop_id, i.delivered_at, i.number, i.total, i.tax_total,
		       CAST(COALESCE((SELECT SUM(il.quantity * il.unit_cost) FROM invoice_lines il WHERE il.company_id = i.company_id AND il.invoice_id = i.id), 0) AS BIGINT),
		       i.customer_id, i.supplier_id, i.delivered_by
		FROM invoices i WHERE ` + onlyId(where, "i", arguments, onlyInvoiceId) + ` ORDER BY i.delivered_at, i.id`
	invoiceRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(invoiceRows, queryError, "delivered invoices", func(rows *sql.Rows) (InvoicePosting, error) {
		invoicePosting := InvoicePosting{CompanyId: companyId}
		customerId := uuid.NullUUID{}
		supplierId := uuid.NullUUID{}
		scanError := rows.Scan(&invoicePosting.InvoiceId, &invoicePosting.ShopId, &invoicePosting.DeliveredAt, &invoicePosting.Number,
			&invoicePosting.Total, &invoicePosting.TaxTotal, &invoicePosting.CostTotal, &customerId, &supplierId, &invoicePosting.UserId)
		invoicePosting.PartyType, invoicePosting.PartyId = PartyCustomer, customerId.UUID
		if supplierId.Valid {
			invoicePosting.PartyType, invoicePosting.PartyId = PartySupplier, supplierId.UUID
		}
		return invoicePosting, scanError
	})
}

func (repository *Repository) UnpostedInvoiceCancels(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyInvoiceId *uuid.UUID) ([]ReversalPosting, error) {
	arguments := &queryArguments{}
	sincePlaceholder := arguments.add(since)
	where := `i.company_id = ` + arguments.add(companyId) + ` AND i.status = 'cancelled' AND i.delivered_at IS NOT NULL AND i.cancelled_at >= ` + sincePlaceholder +
		notPosted("i", SourceInvoiceCancel) + originalPostedOrPending("i", SourceInvoice, "delivered_at", sincePlaceholder)
	query := `SELECT i.id, i.cancelled_at, i.cancel_reason, i.cancelled_by FROM invoices i WHERE ` + onlyId(where, "i", arguments, onlyInvoiceId)
	cancelRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	return scanAll(cancelRows, queryError, "cancelled invoices", func(rows *sql.Rows) (ReversalPosting, error) {
		reversal := ReversalPosting{CompanyId: companyId, OriginalSourceType: SourceInvoice, SourceType: SourceInvoiceCancel}
		reason := sql.NullString{}
		scanError := rows.Scan(&reversal.OriginalSourceId, &reversal.ReversedAt, &reason, &reversal.UserId)
		reversal.Reason = reason.String
		return reversal, scanError
	})
}
