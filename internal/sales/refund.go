package sales

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrSaleVoidedNoRefund  = errors.New("this sale was voided, so it cannot be refunded")
	ErrSaleHasRefunds      = errors.New("this sale already has refunds; refund the rest instead of voiding it")
	ErrRefundLineUnknown   = errors.New("one of the refunded items is not on this sale")
	ErrRefundTooMany       = errors.New("you cannot refund more than was sold")
	ErrRefundCreditTooMuch = errors.New("refunding to the customer's account is only possible up to what is still owed on this sale and by the customer")
	ErrRefundCreditNoDebt  = errors.New("this sale was not on credit, so it cannot be refunded to the customer's account")
	ErrRefundRefReused     = errors.New("this refund reference was already used for another sale")
)

func (service *Service) Refund(ctx context.Context, querier database.Querier, principal *identity.Principal, saleId uuid.UUID, request RefundRequest) (SaleView, bool, error) {
	refundedSaleId, lookupError := service.repository.FindRefundSaleByClientRef(ctx, querier, principal.CompanyId, request.ClientRef)
	if lookupError != nil {
		return SaleView{}, false, lookupError
	}
	if refundedSaleId != nil {
		if *refundedSaleId != saleId {
			return SaleView{}, false, ErrRefundRefReused
		}
		replayedSale, replayError := service.Get(ctx, querier, principal.CompanyId, saleId)
		return replayedSale, false, replayError
	}

	saleView, getError := service.Get(ctx, querier, principal.CompanyId, saleId)
	if getError != nil {
		return SaleView{}, false, getError
	}
	if saleView.VoidedAt != nil {
		return SaleView{}, false, ErrSaleVoidedNoRefund
	}

	itemsById, itemsError := service.repository.RefundableItems(ctx, querier, principal.CompanyId, saleId)
	if itemsError != nil {
		return SaleView{}, false, itemsError
	}

	refundedAt := time.Now().UTC()
	refund := newRefund{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		SaleId:    saleId,
		ClientRef: request.ClientRef,
		Method:    request.Method,
		Restocked: request.Restock,
		Reason:    strings.TrimSpace(request.Reason),
		CreatedBy: principal.UserId,
		CreatedAt: refundedAt,
	}
	seenItems := map[uuid.UUID]bool{}
	for _, lineRequest := range request.Lines {
		itemId := uuid.MustParse(lineRequest.ItemId)
		item, isOnSale := itemsById[itemId]
		if !isOnSale || seenItems[itemId] {
			return SaleView{}, false, ErrRefundLineUnknown
		}
		seenItems[itemId] = true

		quantityLeft := item.Quantity - item.RefundedQuantity
		if lineRequest.Quantity > quantityLeft {
			return SaleView{}, false, ErrRefundTooMany
		}
		refundLine := newRefundLine{
			SaleItemId: itemId,
			Quantity:   lineRequest.Quantity,
			Amount:     refundLineAmount(item, lineRequest.Quantity),
			CostAmount: item.UnitCost * int64(lineRequest.Quantity),
		}
		refund.Lines = append(refund.Lines, refundLine)
		refund.Amount += refundLine.Amount
		refund.CostAmount += refundLine.CostAmount
	}
	refund.TaxAmount = IncludedTax(refund.Amount, saleView.TaxRateBasisPoints)

	if refund.Method == "credit" {
		creditCheckError := service.checkCreditRefund(ctx, querier, principal.CompanyId, saleView, refund.Amount)
		if creditCheckError != nil {
			return SaleView{}, false, creditCheckError
		}
	}

	insertError := service.repository.InsertRefund(ctx, querier, refund)
	if insertError != nil {
		return SaleView{}, false, insertError
	}

	if refund.Restocked {
		for _, refundLine := range refund.Lines {
			returnMovement := stock.MovementRequest{
				CompanyId: principal.CompanyId,
				ShopId:    saleView.ShopId,
				ProductId: itemsById[refundLine.SaleItemId].ProductId,
				Change:    refundLine.Quantity,
				Reason:    "sale",
				Reference: &saleView.ReceiptNumber,
				UserId:    &principal.UserId,
			}
			_, movementError := service.stockService.RecordMovement(ctx, querier, returnMovement)
			if movementError != nil {
				return SaleView{}, false, movementError
			}
		}
	}

	refundPosting := accounting.SaleRefundPosting{
		CompanyId:  principal.CompanyId,
		RefundId:   refund.Id,
		ShopId:     saleView.ShopId,
		RefundedAt: refundedAt,
		Method:     refund.Method,
		Amount:     refund.Amount,
		TaxAmount:  refund.TaxAmount,
		CostAmount: refund.CostAmount,
		Restocked:  refund.Restocked,
		CustomerId: saleView.CustomerId,
		Reference:  "Refund " + saleView.ReceiptNumber + " · " + refund.Reason,
		UserId:     &principal.UserId,
	}
	_, postError := service.ledger.PostSaleRefund(ctx, querier, refundPosting)
	if postError != nil {
		return SaleView{}, false, postError
	}

	if saleView.Fiscal != nil {
		queueError := service.repository.InsertFiscalPending(ctx, querier, FiscalRefundNote, principal.CompanyId, refund.Id, refundedAt)
		if queueError != nil {
			return SaleView{}, false, queueError
		}
	}

	refundedSale, refreshError := service.Get(ctx, querier, principal.CompanyId, saleId)
	return refundedSale, true, refreshError
}

func refundLineAmount(item refundableItem, quantity int) int64 {
	isRefundingTheRest := quantity == item.Quantity-item.RefundedQuantity
	if isRefundingTheRest {
		return item.LineTotal - item.RefundedAmount
	}
	return multiplyDivideRoundHalfUp(item.LineTotal, int64(quantity), int64(item.Quantity))
}

func (service *Service) checkCreditRefund(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleView SaleView, amount int64) error {
	if saleView.CreditAmount == 0 {
		return ErrRefundCreditNoDebt
	}
	creditRefunded, creditError := service.repository.CreditRefunded(ctx, querier, companyId, saleView.Id)
	if creditError != nil {
		return creditError
	}
	if amount > saleView.CreditAmount-creditRefunded {
		return ErrRefundCreditTooMuch
	}
	if saleView.CustomerId == nil {
		return ErrRefundCreditNoDebt
	}
	customerBalance, balanceError := service.customersService.Balance(ctx, querier, companyId, *saleView.CustomerId)
	if balanceError != nil {
		return balanceError
	}
	if amount > customerBalance {
		return ErrRefundCreditTooMuch
	}
	return nil
}
