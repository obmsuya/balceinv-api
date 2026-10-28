package invoices

import (
	"context"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/google/uuid"
)

func (service *BillService) recordSupplierMoney(ctx context.Context, querier database.Querier, principal *identity.Principal, foundInvoice *InvoiceView, request PaymentRequest, newPayment *Payment) error {
	if request.Method == MethodOffset {
		return nil
	}
	isSupplierMethod := request.Method == "cash" || request.Method == "bank" || request.Method == "mobile"
	if !isSupplierMethod {
		return ErrWrongMethod
	}

	reference := foundInvoice.Number
	if trimmedReference := trimmedOrNil(request.Reference); trimmedReference != nil {
		reference += " · " + *trimmedReference
	}
	receiptId := uuid.Must(uuid.NewV7())
	query := `
		INSERT INTO supplier_receipts (id, company_id, supplier_id, shop_id, amount, method, reference, received_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, insertError := querier.ExecContext(ctx, query, receiptId, principal.CompanyId, foundInvoice.Party.Id, foundInvoice.ShopId,
		request.Amount, request.Method, reference, time.Now().UTC(), principal.UserId)
	if insertError != nil {
		return fmt.Errorf("failed to save money from the supplier: %w", insertError)
	}
	postError := service.ledger.PostSupplierReceiptById(ctx, querier, principal.CompanyId, receiptId)
	if postError != nil {
		return postError
	}
	newPayment.SupplierReceiptId = &receiptId
	return nil
}

func (service *BillService) voidSupplierReceipt(ctx context.Context, querier database.Querier, principal *identity.Principal, receiptId uuid.UUID, reason string) error {
	query := `UPDATE supplier_receipts SET voided_at = $3, voided_by = $4, void_reason = $5 WHERE company_id = $1 AND id = $2 AND voided_at IS NULL`
	_, voidError := querier.ExecContext(ctx, query, principal.CompanyId, receiptId, time.Now().UTC(), principal.UserId, reason)
	if voidError != nil {
		return fmt.Errorf("failed to cancel money from the supplier: %w", voidError)
	}
	return service.ledger.PostSupplierReceiptVoidById(ctx, querier, principal.CompanyId, receiptId)
}
