package sales

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrNoActiveShop      = errors.New("choose a shop first")
	ErrShopClosed        = errors.New("this shop is closed; reopen it or switch shops to sell")
	ErrProductNotFound   = errors.New("one of the products is not for sale")
	ErrAddonNotFound     = errors.New("one of the add-ons is not available for that product")
	ErrSaleNotFound      = errors.New("sale not found")
	ErrClientRefReused   = errors.New("this checkout reference was already used for a different sale")
	ErrClientRefInFlight = errors.New("this checkout is already being saved; try again in a moment")
	ErrDuplicatePayment  = errors.New("each payment method can appear only once")
	ErrPaymentTooLow     = errors.New("the payments do not cover the total")
	ErrChangeWithoutCash = errors.New("change can only be given from cash; card and mobile payments cannot exceed what is owed")
	ErrMissingSettings   = errors.New("company settings are missing")
)

type Service struct {
	repository         *Repository
	discountsService   *discounts.Service
	settingsRepository *settings.Repository
	stockService       *stock.Service
}

func NewService(repository *Repository, discountsService *discounts.Service, settingsRepository *settings.Repository, stockService *stock.Service) *Service {
	return &Service{
		repository:         repository,
		discountsService:   discountsService,
		settingsRepository: settingsRepository,
		stockService:       stockService,
	}
}

func (service *Service) Quote(ctx context.Context, querier database.Querier, principal *identity.Principal, request QuoteRequest) (QuoteView, error) {
	if principal.ShopId == nil {
		return QuoteView{}, ErrNoActiveShop
	}

	companySettings, settingsError := service.findSettings(ctx, querier, principal.CompanyId)
	if settingsError != nil {
		return QuoteView{}, settingsError
	}

	pricedSale, priceError := service.price(ctx, querier, principal, request.Items, companySettings.TaxRateBasisPoints)
	if priceError != nil {
		return QuoteView{}, priceError
	}

	quoteView := QuoteView{
		Lines:              make([]LineView, 0, len(pricedSale.Lines)),
		Subtotal:           pricedSale.Subtotal,
		DiscountTotal:      pricedSale.DiscountTotal,
		Total:              pricedSale.Total,
		TaxTotal:           pricedSale.TaxTotal,
		TaxRateBasisPoints: pricedSale.TaxRateBasisPoints,
	}
	for _, pricedLine := range pricedSale.Lines {
		lineView := toLineView(pricedLine)
		inStock := pricedLine.Product.InStock
		lineView.InStock = &inStock
		quoteView.Lines = append(quoteView.Lines, lineView)
	}
	return quoteView, nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request SaleRequest) (SaleView, error) {
	if principal.ShopId == nil {
		return SaleView{}, ErrNoActiveShop
	}

	requestHash, hashError := hashRequest(request)
	if hashError != nil {
		return SaleView{}, hashError
	}
	existingSaleId, existingHash, lookupError := service.repository.FindHashByClientRef(ctx, querier, principal.CompanyId, request.ClientRef)
	if lookupError != nil {
		return SaleView{}, lookupError
	}
	if existingSaleId != nil {
		if existingHash != requestHash {
			return SaleView{}, ErrClientRefReused
		}
		return service.Get(ctx, querier, principal.CompanyId, *existingSaleId)
	}

	companySettings, settingsError := service.findSettings(ctx, querier, principal.CompanyId)
	if settingsError != nil {
		return SaleView{}, settingsError
	}
	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, querier, principal.CompanyId)
	if profileError != nil {
		return SaleView{}, profileError
	}
	if companyProfile == nil {
		return SaleView{}, ErrMissingSettings
	}

	pricedSale, priceError := service.price(ctx, querier, principal, request.Items, companySettings.TaxRateBasisPoints)
	if priceError != nil {
		return SaleView{}, priceError
	}

	payments, amountPaid, changeGiven, paymentError := settlePayments(request.Payments, pricedSale.Total)
	if paymentError != nil {
		return SaleView{}, paymentError
	}

	shopCounter, counterError := service.repository.TakeReceiptCounter(ctx, querier, principal.CompanyId, *principal.ShopId)
	if counterError != nil {
		return SaleView{}, counterError
	}
	if shopCounter == nil {
		return SaleView{}, ErrShopClosed
	}

	createdAt := time.Now().UTC()
	newSale := Sale{
		Id:                 uuid.Must(uuid.NewV7()),
		CompanyId:          principal.CompanyId,
		ShopId:             *principal.ShopId,
		UserId:             principal.UserId,
		ClientRef:          request.ClientRef,
		RequestHash:        requestHash,
		ReceiptNumber:      formatReceiptNumber(companySettings.ReceiptNumberFormat, *shopCounter, createdAt, companyProfile.Timezone),
		Subtotal:           pricedSale.Subtotal,
		DiscountTotal:      pricedSale.DiscountTotal,
		Total:              pricedSale.Total,
		TaxTotal:           pricedSale.TaxTotal,
		TaxRateBasisPoints: pricedSale.TaxRateBasisPoints,
		AmountPaid:         amountPaid,
		ChangeGiven:        changeGiven,
		CurrencyCode:       companyProfile.CurrencyCode,
		CurrencyDecimals:   companyProfile.CurrencyDecimals,
		Note:               trimmedOrNil(request.Note),
		CreatedAt:          createdAt,
	}

	insertSaleError := service.repository.InsertSale(ctx, querier, newSale)
	if database.IsUniqueViolation(insertSaleError) {
		return SaleView{}, ErrClientRefInFlight
	}
	if insertSaleError != nil {
		return SaleView{}, insertSaleError
	}

	insertLinesError := service.repository.InsertLines(ctx, querier, principal.CompanyId, newSale.Id, pricedSale.Lines)
	if insertLinesError != nil {
		return SaleView{}, insertLinesError
	}
	insertPaymentsError := service.repository.InsertPayments(ctx, querier, principal.CompanyId, newSale.Id, payments)
	if insertPaymentsError != nil {
		return SaleView{}, insertPaymentsError
	}
	if companySettings.EfdEnabled {
		queueError := service.repository.InsertFiscalPending(ctx, querier, principal.CompanyId, newSale.Id, createdAt)
		if queueError != nil {
			return SaleView{}, queueError
		}
	}

	for _, pricedLine := range pricedSale.Lines {
		saleMovement := stock.MovementRequest{
			CompanyId: principal.CompanyId,
			ShopId:    *principal.ShopId,
			ProductId: pricedLine.Product.Id,
			Change:    -pricedLine.Quantity,
			Reason:    "sale",
			Reference: &newSale.ReceiptNumber,
			UserId:    &principal.UserId,
		}
		_, movementError := service.stockService.RecordMovement(ctx, querier, saleMovement)
		if movementError != nil {
			return SaleView{}, movementError
		}
	}

	return service.Get(ctx, querier, principal.CompanyId, newSale.Id)
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) (SaleView, error) {
	saleView, findError := service.repository.FindView(ctx, querier, companyId, saleId)
	if findError != nil {
		return SaleView{}, findError
	}
	if saleView == nil {
		return SaleView{}, ErrSaleNotFound
	}

	lineViews, linesError := service.repository.ListLines(ctx, querier, companyId, saleId)
	if linesError != nil {
		return SaleView{}, linesError
	}
	paymentViews, paymentsError := service.repository.ListPayments(ctx, querier, companyId, saleId)
	if paymentsError != nil {
		return SaleView{}, paymentsError
	}

	fiscalView, fiscalError := service.repository.FindFiscal(ctx, querier, companyId, saleId)
	if fiscalError != nil {
		return SaleView{}, fiscalError
	}

	saleView.Items = lineViews
	saleView.Payments = paymentViews
	saleView.Fiscal = fiscalView
	return *saleView, nil
}

func (service *Service) price(ctx context.Context, querier database.Querier, principal *identity.Principal, lineRequests []LineRequest, taxRateBasisPoints int) (PricedSale, error) {
	productIds, addonIds := collectIds(lineRequests)

	productsById, productsError := service.repository.LoadProducts(ctx, querier, principal.CompanyId, *principal.ShopId, productIds)
	if productsError != nil {
		return PricedSale{}, productsError
	}
	addonsById, addonsError := service.repository.LoadAddons(ctx, querier, principal.CompanyId, addonIds)
	if addonsError != nil {
		return PricedSale{}, addonsError
	}

	pricingLines := make([]PricingLine, 0, len(lineRequests))
	for _, lineRequest := range lineRequests {
		pricingProduct, isKnownProduct := productsById[uuid.MustParse(lineRequest.ProductId)]
		if !isKnownProduct {
			return PricedSale{}, ErrProductNotFound
		}

		lineAddons := make([]PricingAddon, 0, len(lineRequest.AddonIds))
		seenAddons := map[uuid.UUID]bool{}
		for _, rawAddonId := range lineRequest.AddonIds {
			addonId := uuid.MustParse(rawAddonId)
			pricingAddon, isKnownAddon := addonsById[addonId]
			isUsable := isKnownAddon && pricingAddon.ProductId == pricingProduct.Id && !seenAddons[addonId]
			if !isUsable {
				return PricedSale{}, ErrAddonNotFound
			}
			seenAddons[addonId] = true
			lineAddons = append(lineAddons, pricingAddon)
		}

		pricingLines = append(pricingLines, PricingLine{
			Product:  pricingProduct,
			Quantity: lineRequest.Quantity,
			Addons:   lineAddons,
		})
	}

	applicableDiscounts, discountsError := service.discountsService.Applicable(ctx, querier, principal.CompanyId, productIds, time.Now().UTC())
	if discountsError != nil {
		return PricedSale{}, discountsError
	}

	return PriceSale(pricingLines, applicableDiscounts, taxRateBasisPoints), nil
}

func (service *Service) TillOptions(ctx context.Context, querier database.Querier, companyId uuid.UUID) (TillOptionsView, error) {
	companySettings, settingsError := service.findSettings(ctx, querier, companyId)
	if settingsError != nil {
		return TillOptionsView{}, settingsError
	}

	tillOptions := TillOptionsView{
		NumpadEnabled:             companySettings.TillNumpadEnabled,
		CustomerDisplayEnabled:    companySettings.CustomerDisplayEnabled,
		EfdEnabled:                companySettings.EfdEnabled,
		PrintReceiptAutomatically: companySettings.PrintReceiptAutomatically,
	}
	return tillOptions, nil
}

func (service *Service) findSettings(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*settings.Settings, error) {
	companySettings, settingsError := service.settingsRepository.FindSettings(ctx, querier, companyId)
	if settingsError != nil {
		return nil, settingsError
	}
	if companySettings == nil {
		return nil, ErrMissingSettings
	}
	return companySettings, nil
}

func settlePayments(paymentRequests []PaymentRequest, total int64) ([]Payment, int64, int64, error) {
	payments := make([]Payment, 0, len(paymentRequests))
	seenMethods := map[string]bool{}
	cashAmount := int64(0)
	nonCashAmount := int64(0)

	for _, paymentRequest := range paymentRequests {
		if seenMethods[paymentRequest.Method] {
			return nil, 0, 0, ErrDuplicatePayment
		}
		seenMethods[paymentRequest.Method] = true
		payments = append(payments, Payment{Method: paymentRequest.Method, Amount: paymentRequest.Amount})
		if paymentRequest.Method == PaymentCash {
			cashAmount += paymentRequest.Amount
		} else {
			nonCashAmount += paymentRequest.Amount
		}
	}

	amountPaid := cashAmount + nonCashAmount
	if amountPaid < total {
		return nil, 0, 0, ErrPaymentTooLow
	}
	if nonCashAmount > total {
		return nil, 0, 0, ErrChangeWithoutCash
	}

	return payments, amountPaid, amountPaid - total, nil
}

func formatReceiptNumber(receiptFormat string, shopCounter ShopCounter, soldAt time.Time, timezone string) string {
	companyLocation, locationError := time.LoadLocation(timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}

	receiptNumber := strings.ReplaceAll(receiptFormat, "{SHOP}", shopCounter.ReceiptPrefix)
	receiptNumber = strings.ReplaceAll(receiptNumber, "{DATE}", soldAt.In(companyLocation).Format("20060102"))
	receiptNumber = strings.ReplaceAll(receiptNumber, "{COUNTER}", fmt.Sprintf("%04d", shopCounter.Number))
	return receiptNumber
}

func hashRequest(request SaleRequest) (string, error) {
	hashedFields := struct {
		Items    []LineRequest    `json:"items"`
		Payments []PaymentRequest `json:"payments"`
		Note     *string          `json:"note"`
	}{
		Items:    request.Items,
		Payments: request.Payments,
		Note:     request.Note,
	}

	encodedFields, encodeError := json.Marshal(hashedFields)
	if encodeError != nil {
		return "", fmt.Errorf("failed to fingerprint the sale: %w", encodeError)
	}
	fieldsHash := sha256.Sum256(encodedFields)
	return hex.EncodeToString(fieldsHash[:]), nil
}

func collectIds(lineRequests []LineRequest) ([]uuid.UUID, []uuid.UUID) {
	productIds := []uuid.UUID{}
	addonIds := []uuid.UUID{}
	seenProducts := map[uuid.UUID]bool{}
	seenAddons := map[uuid.UUID]bool{}

	for _, lineRequest := range lineRequests {
		productId := uuid.MustParse(lineRequest.ProductId)
		if !seenProducts[productId] {
			seenProducts[productId] = true
			productIds = append(productIds, productId)
		}
		for _, rawAddonId := range lineRequest.AddonIds {
			addonId := uuid.MustParse(rawAddonId)
			if !seenAddons[addonId] {
				seenAddons[addonId] = true
				addonIds = append(addonIds, addonId)
			}
		}
	}

	return productIds, addonIds
}

func toLineView(pricedLine PricedLine) LineView {
	addonViews := make([]AddonView, 0, len(pricedLine.Addons))
	for _, addon := range pricedLine.Addons {
		addonViews = append(addonViews, AddonView{AddonId: addon.Id, Name: addon.Name, UnitPrice: addon.Price})
	}

	return LineView{
		ProductId:       pricedLine.Product.Id,
		ProductName:     pricedLine.Product.Name,
		VariantLabel:    pricedLine.Product.VariantLabel,
		Sku:             pricedLine.Product.Sku,
		Unit:            pricedLine.Product.Unit,
		Quantity:        pricedLine.Quantity,
		UnitPrice:       pricedLine.UnitPrice,
		IsWholesale:     pricedLine.IsWholesale,
		Addons:          addonViews,
		AddonsUnitTotal: pricedLine.AddonsUnitTotal,
		DiscountName:    pricedLine.DiscountName,
		DiscountAmount:  pricedLine.DiscountAmount,
		LineTotal:       pricedLine.LineTotal,
	}
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return nil
	}
	return &trimmedValue
}
