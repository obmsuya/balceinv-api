package suppliers

import (
	"context"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

func (service *Service) supplierBalance(ctx context.Context, querier database.Querier, companyId uuid.UUID, foundSupplier Supplier) (int64, error) {
	ledgers, ledgerError := service.repository.LoadLedgers(ctx, querier, companyId, []Supplier{foundSupplier}, true)
	if ledgerError != nil {
		return 0, ledgerError
	}
	openDebits, unappliedCredit := Allocate(ledgers[foundSupplier.Id])
	return Balance(openDebits, unappliedCredit), nil
}

func (service *Service) RecordPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, request PaymentRequest) (PaymentView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return PaymentView{}, featureError
	}

	foundSupplier, supplierError := service.findUsableSupplier(ctx, querier, principal.CompanyId, uuid.MustParse(request.SupplierId), false)
	if supplierError != nil {
		return PaymentView{}, supplierError
	}
	shopId, shopError := service.resolveShop(ctx, querier, principal, request.ShopId)
	if shopError != nil {
		return PaymentView{}, shopError
	}
	createdAt := time.Now().UTC()
	paidAt, momentError := resolveMoment(request.PaidAt, createdAt)
	if momentError != nil {
		return PaymentView{}, momentError
	}

	var purchaseId *uuid.UUID
	if request.PurchaseId != nil {
		foundPurchase, findError := service.repository.FindPurchase(ctx, querier, principal.CompanyId, uuid.MustParse(*request.PurchaseId))
		if findError != nil {
			return PaymentView{}, findError
		}
		isSupplierPurchase := foundPurchase != nil && foundPurchase.SupplierId != nil && *foundPurchase.SupplierId == foundSupplier.Id
		if !isSupplierPurchase {
			return PaymentView{}, ErrPurchaseNotFound
		}
		if foundPurchase.Status == PurchaseCancelled {
			return PaymentView{}, ErrAlreadyCancelled
		}
		purchaseId = &foundPurchase.Id
	}

	currentBalance, balanceError := service.supplierBalance(ctx, querier, principal.CompanyId, *foundSupplier)
	if balanceError != nil {
		return PaymentView{}, balanceError
	}
	if request.Amount > currentBalance {
		return PaymentView{}, ErrOverpayment
	}

	paymentId, insertError := service.insertPayment(ctx, querier, Payment{
		CompanyId:  principal.CompanyId,
		SupplierId: &foundSupplier.Id,
		PurchaseId: purchaseId,
		ShopId:     shopId,
		Amount:     request.Amount,
		Method:     request.Method,
		Reference:  trimmedOrNil(request.Reference),
		PaidAt:     paidAt,
		CreatedBy:  principal.UserId,
		CreatedAt:  createdAt,
	})
	if insertError != nil {
		return PaymentView{}, insertError
	}

	return service.paymentView(ctx, querier, principal.CompanyId, paymentId)
}

func (service *Service) paymentView(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) (PaymentView, error) {
	foundPayment, findError := service.repository.FindPayment(ctx, querier, companyId, paymentId)
	if findError != nil {
		return PaymentView{}, findError
	}
	if foundPayment == nil {
		return PaymentView{}, ErrPaymentNotFound
	}
	return *foundPayment, nil
}

func (service *Service) VoidPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, paymentId uuid.UUID, request ReasonRequest) (PaymentView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return PaymentView{}, featureError
	}
	voidReason, reasonError := requireReason(request.Reason)
	if reasonError != nil {
		return PaymentView{}, reasonError
	}

	foundPayment, findError := service.paymentView(ctx, querier, principal.CompanyId, paymentId)
	if findError != nil {
		return PaymentView{}, findError
	}
	if foundPayment.IsVoided {
		return PaymentView{}, ErrPaymentAlreadyVoided
	}
	if foundPayment.SupplierId == nil {
		return PaymentView{}, ErrPaymentLocked
	}

	voidError := service.repository.VoidPayment(ctx, querier, principal.CompanyId, paymentId, principal.UserId, voidReason, time.Now().UTC())
	if voidError != nil {
		return PaymentView{}, voidError
	}
	voidPostError := service.ledger.PostSupplierPaymentVoidById(ctx, querier, principal.CompanyId, paymentId)
	if voidPostError != nil {
		return PaymentView{}, voidPostError
	}
	return service.paymentView(ctx, querier, principal.CompanyId, paymentId)
}

func (service *Service) ListPayments(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId *uuid.UUID, limit int, offset int) (response.Page[PaymentView], error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return response.Page[PaymentView]{}, featureError
	}

	totalPayments, countError := service.repository.CountPayments(ctx, querier, principal.CompanyId, supplierId)
	if countError != nil {
		return response.Page[PaymentView]{}, countError
	}
	paymentViews, listError := service.repository.ListPayments(ctx, querier, principal.CompanyId, supplierId, limit, offset)
	if listError != nil {
		return response.Page[PaymentView]{}, listError
	}

	paymentPage := response.Page[PaymentView]{
		Items:  paymentViews,
		Total:  totalPayments,
		Limit:  limit,
		Offset: offset,
	}
	return paymentPage, nil
}

func (service *Service) RecordReturn(ctx context.Context, querier database.Querier, principal *identity.Principal, request ReturnRequest) (ReturnView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return ReturnView{}, featureError
	}

	foundSupplier, supplierError := service.findUsableSupplier(ctx, querier, principal.CompanyId, uuid.MustParse(request.SupplierId), false)
	if supplierError != nil {
		return ReturnView{}, supplierError
	}
	shopId, shopError := service.resolveShop(ctx, querier, principal, request.ShopId)
	if shopError != nil {
		return ReturnView{}, shopError
	}

	rawProductIds := make([]string, 0, len(request.Lines))
	for _, lineRequest := range request.Lines {
		rawProductIds = append(rawProductIds, lineRequest.ProductId)
	}
	productIds, productsError := service.validateProducts(ctx, querier, principal.CompanyId, rawProductIds)
	if productsError != nil {
		return ReturnView{}, productsError
	}

	createdAt := time.Now().UTC()
	newReturn := SupplierReturn{
		Id:         uuid.Must(uuid.NewV7()),
		CompanyId:  principal.CompanyId,
		SupplierId: foundSupplier.Id,
		ShopId:     shopId,
		Note:       trimmedOrNil(request.Note),
		ReturnedAt: createdAt,
		CreatedBy:  principal.UserId,
		CreatedAt:  createdAt,
	}
	returnLines := make([]ReturnLine, 0, len(request.Lines))
	for lineIndex, lineRequest := range request.Lines {
		returnLines = append(returnLines, ReturnLine{
			ProductId: productIds[lineIndex],
			Quantity:  lineRequest.Quantity,
			UnitCost:  lineRequest.UnitCost,
		})
		newReturn.Total += int64(lineRequest.Quantity) * lineRequest.UnitCost
	}

	returnNumber, numberError := service.repository.TakeDocumentNumber(ctx, querier, principal.CompanyId, documentReturn)
	if numberError != nil {
		return ReturnView{}, numberError
	}
	newReturn.ReturnNumber = returnNumber
	insertError := service.repository.InsertReturn(ctx, querier, newReturn, returnLines)
	if insertError != nil {
		return ReturnView{}, insertError
	}

	for _, returnLine := range returnLines {
		returnMovement := stock.MovementRequest{
			CompanyId: principal.CompanyId,
			ShopId:    shopId,
			ProductId: returnLine.ProductId,
			Change:    -returnLine.Quantity,
			Reason:    stockReasonReturn,
			Reference: &newReturn.ReturnNumber,
			UserId:    &principal.UserId,
		}
		_, movementError := service.stockService.RecordMovement(ctx, querier, returnMovement)
		if movementError != nil {
			return ReturnView{}, movementError
		}
	}
	postError := service.ledger.PostSupplierReturnById(ctx, querier, principal.CompanyId, newReturn.Id)
	if postError != nil {
		return ReturnView{}, postError
	}

	return service.returnView(ctx, querier, principal.CompanyId, newReturn.Id)
}

func (service *Service) GetReturn(ctx context.Context, querier database.Querier, principal *identity.Principal, returnId uuid.UUID) (ReturnView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return ReturnView{}, featureError
	}
	return service.returnView(ctx, querier, principal.CompanyId, returnId)
}

func (service *Service) returnView(ctx context.Context, querier database.Querier, companyId uuid.UUID, returnId uuid.UUID) (ReturnView, error) {
	foundReturn, findError := service.repository.FindReturn(ctx, querier, companyId, returnId)
	if findError != nil {
		return ReturnView{}, findError
	}
	if foundReturn == nil {
		return ReturnView{}, ErrReturnNotFound
	}
	returnLines, linesError := service.repository.ListReturnLines(ctx, querier, companyId, returnId)
	if linesError != nil {
		return ReturnView{}, linesError
	}
	foundReturn.Lines = returnLines
	return *foundReturn, nil
}

func (service *Service) ListReturns(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId *uuid.UUID, limit int, offset int) (response.Page[ReturnView], error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return response.Page[ReturnView]{}, featureError
	}

	totalReturns, countError := service.repository.CountReturns(ctx, querier, principal.CompanyId, supplierId)
	if countError != nil {
		return response.Page[ReturnView]{}, countError
	}
	returnViews, listError := service.repository.ListReturns(ctx, querier, principal.CompanyId, supplierId, limit, offset)
	if listError != nil {
		return response.Page[ReturnView]{}, listError
	}

	returnPage := response.Page[ReturnView]{
		Items:  returnViews,
		Total:  totalReturns,
		Limit:  limit,
		Offset: offset,
	}
	return returnPage, nil
}
