package invoices

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrInvoiceNotFound       = errors.New("invoice not found")
	ErrNoActiveShop          = errors.New("choose a shop first")
	ErrInvoicesOff           = errors.New("invoices are switched off; turn them on in Settings, Features")
	ErrCustomersOff          = errors.New("customer invoices need Customers switched on")
	ErrSuppliersOff          = errors.New("supplier invoices need Suppliers switched on")
	ErrPartyRequired         = errors.New("choose who the document is for")
	ErrProductNotFound       = errors.New("a product on this document was not found")
	ErrDescriptionRequired   = errors.New("write what each typed line is for")
	ErrPriceRequired         = errors.New("give each typed line a price")
	ErrDiscountTooHigh       = errors.New("a line discount cannot be more than the line")
	ErrInvalidDate           = errors.New("a date is not valid")
	ErrDueBeforeIssue        = errors.New("the due date cannot be before the issue date")
	ErrNotDraft              = errors.New("only a draft can be changed")
	ErrInvalidStatus         = errors.New("this document cannot do that in its current state")
	ErrNotABill              = errors.New("only invoices can be delivered or paid")
	ErrAlreadyDelivered      = errors.New("this invoice was already delivered")
	ErrPaymentTooHigh        = errors.New("the payment is more than what is still owed")
	ErrWrongMethod           = errors.New("this way of paying does not fit this document")
	ErrPaymentNotFound       = errors.New("payment not found")
	ErrPaymentAlreadyVoided  = errors.New("this payment was already cancelled")
	ErrHasPayments           = errors.New("cancel the payments on this invoice first")
	ErrInvalidReason         = errors.New("write why")
	ErrCannotConvert         = errors.New("this document cannot be turned into that")
	ErrNumberInFlight        = errors.New("another document was saved at the same moment; try again")
	ErrInvalidFilter         = errors.New("a filter is not valid")
	ErrSupplierReceiptsLater = errors.New("money received from suppliers is not available yet")
)

type BillService struct {
	repository       *Repository
	customersService *customers.Service
	stockService     *stock.Service
	ledger           *accounting.Ledger
	documentService  *Service
}

func NewBillService(repository *Repository, customersService *customers.Service, stockService *stock.Service, ledger *accounting.Ledger, documentService *Service) *BillService {
	return &BillService{
		repository:       repository,
		customersService: customersService,
		stockService:     stockService,
		ledger:           ledger,
		documentService:  documentService,
	}
}

type companyContext struct {
	features features.Features
	settings settings.Settings
	profile  settings.CompanyProfile
	location *time.Location
}

func (service *BillService) loadCompany(ctx context.Context, querier database.Querier, companyId uuid.UUID) (companyContext, error) {
	companyFeatures, featuresError := features.NewRepository().Find(ctx, querier, companyId)
	if featuresError != nil {
		return companyContext{}, fmt.Errorf("failed to read the features for an invoice: %w", featuresError)
	}
	settingsRepository := settings.NewRepository()
	companySettings, settingsError := settingsRepository.FindSettings(ctx, querier, companyId)
	if settingsError != nil {
		return companyContext{}, settingsError
	}
	companyProfile, profileError := settingsRepository.FindCompanyProfile(ctx, querier, companyId)
	if profileError != nil {
		return companyContext{}, profileError
	}
	if companySettings == nil || companyProfile == nil {
		return companyContext{}, sales.ErrMissingSettings
	}
	companyLocation, locationError := time.LoadLocation(companyProfile.Timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	return companyContext{features: companyFeatures, settings: *companySettings, profile: *companyProfile, location: companyLocation}, nil
}

func (company companyContext) today() string {
	return time.Now().In(company.location).Format(time.DateOnly)
}

func (company companyContext) requireKind(kind string) error {
	if !company.features.InvoicesEnabled {
		return ErrInvoicesOff
	}
	if kind == KindSupplierInvoice && !company.features.SuppliersEnabled {
		return ErrSuppliersOff
	}
	if kind != KindSupplierInvoice && !company.features.CustomersEnabled {
		return ErrCustomersOff
	}
	return nil
}

func (company companyContext) taxRate() int {
	if !company.features.VatRegistered {
		return 0
	}
	return company.settings.TaxRateBasisPoints
}

func (service *BillService) List(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string, filter Filter, limit int, offset int) (response.Page[InvoiceView], error) {
	company, companyError := service.loadCompany(ctx, querier, principal.CompanyId)
	if companyError != nil {
		return response.Page[InvoiceView]{}, companyError
	}
	if !company.features.InvoicesEnabled {
		return response.Page[InvoiceView]{}, ErrInvoicesOff
	}
	isKnownKind := filter.Kind == "" || kindPrefixes[filter.Kind] != ""
	isKnownStatus := filter.Status == "" || filter.Status == StatusDraft || filter.Status == StatusIssued || filter.Status == StatusAccepted || filter.Status == StatusCancelled
	isKnownPaymentStatus := filter.PaymentStatus == "" || filter.PaymentStatus == PaymentUnpaid || filter.PaymentStatus == PaymentPartPaid || filter.PaymentStatus == PaymentPaid || filter.PaymentStatus == "overdue"
	if !isKnownKind || !isKnownStatus || !isKnownPaymentStatus {
		return response.Page[InvoiceView]{}, ErrInvalidFilter
	}
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return response.Page[InvoiceView]{}, scopeError
	}

	today := company.today()
	invoiceViews, totalInvoices, listError := service.repository.List(ctx, querier, principal.CompanyId, scope, filter, today, limit, offset)
	if listError != nil {
		return response.Page[InvoiceView]{}, listError
	}
	for invoiceIndex := range invoiceViews {
		describePayment(&invoiceViews[invoiceIndex], today)
	}
	return response.Page[InvoiceView]{Items: invoiceViews, Total: totalInvoices, Limit: limit, Offset: offset}, nil
}

func (service *BillService) Summary(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string) (SummaryView, error) {
	company, companyError := service.loadCompany(ctx, querier, principal.CompanyId)
	if companyError != nil {
		return SummaryView{}, companyError
	}
	if !company.features.InvoicesEnabled {
		return SummaryView{}, ErrInvoicesOff
	}
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return SummaryView{}, scopeError
	}
	nowInCompany := time.Now().In(company.location)
	monthStart := time.Date(nowInCompany.Year(), nowInCompany.Month(), 1, 0, 0, 0, 0, company.location).UTC()
	return service.repository.Summary(ctx, querier, principal.CompanyId, scope, company.today(), monthStart)
}

func (service *BillService) Get(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (InvoiceView, error) {
	company, companyError := service.loadCompany(ctx, querier, principal.CompanyId)
	if companyError != nil {
		return InvoiceView{}, companyError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) get(ctx context.Context, querier database.Querier, principal *identity.Principal, company companyContext, invoiceId uuid.UUID) (InvoiceView, error) {
	if !company.features.InvoicesEnabled {
		return InvoiceView{}, ErrInvoicesOff
	}
	foundInvoice, findError := service.repository.Find(ctx, querier, principal.CompanyId, invoiceId)
	if findError != nil {
		return InvoiceView{}, findError
	}
	if foundInvoice == nil {
		return InvoiceView{}, ErrInvoiceNotFound
	}
	_, scopeError := shopscope.Resolve(ctx, querier, principal, foundInvoice.ShopId.String())
	if errors.Is(scopeError, shopscope.ErrShopNotAssigned) {
		return InvoiceView{}, ErrInvoiceNotFound
	}
	if scopeError != nil {
		return InvoiceView{}, scopeError
	}

	lineViews, linesError := service.repository.Lines(ctx, querier, principal.CompanyId, invoiceId)
	if linesError != nil {
		return InvoiceView{}, linesError
	}
	paymentViews, paymentsError := service.repository.Payments(ctx, querier, principal.CompanyId, invoiceId)
	if paymentsError != nil {
		return InvoiceView{}, paymentsError
	}
	derivedViews, derivedError := service.repository.Related(ctx, querier, principal.CompanyId, invoiceId)
	if derivedError != nil {
		return InvoiceView{}, derivedError
	}
	if foundInvoice.Source != nil {
		sourceView, sourceError := service.repository.FindRelated(ctx, querier, principal.CompanyId, foundInvoice.Source.Id)
		if sourceError != nil {
			return InvoiceView{}, sourceError
		}
		foundInvoice.Source = sourceView
	}
	foundInvoice.Lines = lineViews
	foundInvoice.Payments = paymentViews
	foundInvoice.Derived = derivedViews
	describePayment(foundInvoice, company.today())
	return *foundInvoice, nil
}

func describePayment(invoiceView *InvoiceView, today string) {
	invoiceView.BalanceDue = invoiceView.Total - invoiceView.PaidTotal
	isBill := invoiceView.Kind == KindInvoice || invoiceView.Kind == KindSupplierInvoice
	if !isBill || invoiceView.Status == StatusCancelled {
		invoiceView.BalanceDue = 0
		return
	}
	paymentStatus := PaymentUnpaid
	switch {
	case invoiceView.PaidTotal >= invoiceView.Total:
		paymentStatus = PaymentPaid
	case invoiceView.PaidTotal > 0:
		paymentStatus = PaymentPartPaid
	}
	invoiceView.PaymentStatus = &paymentStatus
	isOwed := invoiceView.BalanceDue > 0 && invoiceView.Status == StatusIssued
	invoiceView.IsOverdue = isOwed && invoiceView.DueDate != nil && *invoiceView.DueDate < today
}

func (service *BillService) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request InvoiceRequest) (InvoiceView, error) {
	if principal.ShopId == nil {
		return InvoiceView{}, ErrNoActiveShop
	}
	company, companyError := service.loadCompany(ctx, querier, principal.CompanyId)
	if companyError != nil {
		return InvoiceView{}, companyError
	}
	kindError := company.requireKind(request.Kind)
	if kindError != nil {
		return InvoiceView{}, kindError
	}

	createdAt := time.Now().UTC()
	newInvoice := Invoice{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		ShopId:    *principal.ShopId,
		Kind:      request.Kind,
		Status:    StatusDraft,
		CreatedBy: principal.UserId,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	pricedLines, fillError := service.fill(ctx, querier, principal, company, &newInvoice, request)
	if fillError != nil {
		return InvoiceView{}, fillError
	}

	numberError := service.assignNumber(ctx, querier, company, &newInvoice)
	if numberError != nil {
		return InvoiceView{}, numberError
	}
	insertError := service.repository.Insert(ctx, querier, newInvoice, pricedLines)
	if database.IsUniqueViolation(insertError) {
		return InvoiceView{}, ErrNumberInFlight
	}
	if insertError != nil {
		return InvoiceView{}, insertError
	}
	return service.get(ctx, querier, principal, company, newInvoice.Id)
}

func (service *BillService) assignNumber(ctx context.Context, querier database.Querier, company companyContext, invoice *Invoice) error {
	issueYear := time.Now().In(company.location).Year()
	if parsedIssueDate, parseError := time.Parse(time.DateOnly, invoice.IssueDate); parseError == nil {
		issueYear = parsedIssueDate.Year()
	}
	prefix := kindPrefixes[invoice.Kind]
	takenNumber, takeError := service.repository.TakeNumber(ctx, querier, invoice.CompanyId, prefix, issueYear)
	if takeError != nil {
		return takeError
	}
	invoice.Number = fmt.Sprintf("%s-%d-%04d", prefix, issueYear, takenNumber)
	return nil
}

func (service *BillService) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, request InvoiceRequest) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	if foundInvoice.Status != StatusDraft {
		return InvoiceView{}, ErrNotDraft
	}
	if request.Kind != foundInvoice.Kind {
		return InvoiceView{}, ErrInvalidStatus
	}

	changedInvoice := Invoice{
		Id:        foundInvoice.Id,
		CompanyId: principal.CompanyId,
		ShopId:    foundInvoice.ShopId,
		Kind:      foundInvoice.Kind,
		UpdatedAt: time.Now().UTC(),
	}
	pricedLines, fillError := service.fill(ctx, querier, principal, company, &changedInvoice, request)
	if fillError != nil {
		return InvoiceView{}, fillError
	}
	updateError := service.repository.UpdateDraft(ctx, querier, changedInvoice, pricedLines)
	if updateError != nil {
		return InvoiceView{}, updateError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) fill(ctx context.Context, querier database.Querier, principal *identity.Principal, company companyContext, invoice *Invoice, request InvoiceRequest) ([]Line, error) {
	partyError := service.setParty(ctx, querier, principal, invoice, request)
	if partyError != nil {
		return nil, partyError
	}

	issueDate := company.today()
	if trimmedOrNil(request.IssueDate) != nil {
		issueDate = *trimmedOrNil(request.IssueDate)
	}
	_, issueParseError := time.Parse(time.DateOnly, issueDate)
	if issueParseError != nil {
		return nil, ErrInvalidDate
	}
	invoice.IssueDate = issueDate

	invoice.DueDate = trimmedOrNil(request.DueDate)
	if invoice.DueDate == nil && invoice.Kind != KindDeliveryNote {
		defaultDueDate := mustDate(issueDate).AddDate(0, 0, company.profile.InvoiceDueDays).Format(time.DateOnly)
		invoice.DueDate = &defaultDueDate
	}
	if invoice.DueDate != nil {
		_, dueParseError := time.Parse(time.DateOnly, *invoice.DueDate)
		if dueParseError != nil {
			return nil, ErrInvalidDate
		}
		if *invoice.DueDate < issueDate {
			return nil, ErrDueBeforeIssue
		}
	}

	invoice.Reference = trimmedOrNil(request.Reference)
	invoice.Notes = trimmedOrNil(request.Notes)
	invoice.Terms = trimmedOrNil(request.Terms)
	if request.Terms == nil && company.profile.InvoiceTerms != nil && invoice.Kind != KindDeliveryNote {
		invoice.Terms = trimmedOrNil(company.profile.InvoiceTerms)
	}

	pricedLines, priceError := service.priceLines(ctx, querier, principal.CompanyId, request.Lines)
	if priceError != nil {
		return nil, priceError
	}
	invoice.Subtotal, invoice.DiscountTotal = 0, 0
	for _, pricedLine := range pricedLines {
		invoice.Subtotal += int64(pricedLine.Quantity) * pricedLine.UnitPrice
		invoice.DiscountTotal += pricedLine.DiscountAmount
	}
	invoice.Total = invoice.Subtotal - invoice.DiscountTotal
	invoice.TaxRateBasisPoints = company.taxRate()
	invoice.TaxTotal = sales.IncludedTax(invoice.Total, invoice.TaxRateBasisPoints)
	return pricedLines, nil
}

func (service *BillService) setParty(ctx context.Context, querier database.Querier, principal *identity.Principal, invoice *Invoice, request InvoiceRequest) error {
	if invoice.Kind == KindSupplierInvoice {
		if request.SupplierId == nil {
			return ErrPartyRequired
		}
		supplierId := uuid.MustParse(*request.SupplierId)
		isActive := false
		scanError := querier.QueryRowContext(ctx, `SELECT is_active FROM suppliers WHERE company_id = $1 AND id = $2`, principal.CompanyId, supplierId).Scan(&isActive)
		if errors.Is(scanError, sql.ErrNoRows) || (scanError == nil && !isActive) {
			return ErrPartyRequired
		}
		if scanError != nil {
			return fmt.Errorf("failed to check the supplier: %w", scanError)
		}
		invoice.SupplierId = &supplierId
		invoice.CustomerId = nil
		return nil
	}

	if request.CustomerId == nil {
		return ErrPartyRequired
	}
	customerId := uuid.MustParse(*request.CustomerId)
	customerError := service.customersService.RequireActive(ctx, querier, principal.CompanyId, customerId)
	if customerError != nil {
		return customerError
	}
	invoice.CustomerId = &customerId
	invoice.SupplierId = nil
	return nil
}

func (service *BillService) priceLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, lineRequests []LineRequest) ([]Line, error) {
	pricedLines := []Line{}
	for position, lineRequest := range lineRequests {
		pricedLine := Line{
			Id:             uuid.Must(uuid.NewV7()),
			Position:       position,
			Description:    strings.TrimSpace(lineRequest.Description),
			Unit:           strings.TrimSpace(lineRequest.Unit),
			Quantity:       lineRequest.Quantity,
			DiscountAmount: lineRequest.DiscountAmount,
		}
		if lineRequest.ProductId != nil {
			productId := uuid.MustParse(*lineRequest.ProductId)
			productName, variantLabel, productUnit, productPrice := "", "", "", int64(0)
			scanError := querier.QueryRowContext(ctx, `SELECT name, variant_label, unit, price FROM products WHERE company_id = $1 AND id = $2 AND is_active`, companyId, productId).
				Scan(&productName, &variantLabel, &productUnit, &productPrice)
			if errors.Is(scanError, sql.ErrNoRows) {
				return nil, ErrProductNotFound
			}
			if scanError != nil {
				return nil, fmt.Errorf("failed to price an invoice line: %w", scanError)
			}
			pricedLine.ProductId = &productId
			if pricedLine.Description == "" {
				pricedLine.Description = productName
				if variantLabel != "" {
					pricedLine.Description += " · " + variantLabel
				}
			}
			if pricedLine.Unit == "" {
				pricedLine.Unit = productUnit
			}
			pricedLine.UnitPrice = productPrice
		}
		if lineRequest.UnitPrice != nil {
			pricedLine.UnitPrice = *lineRequest.UnitPrice
		}
		if pricedLine.ProductId == nil && pricedLine.Description == "" {
			return nil, ErrDescriptionRequired
		}
		if pricedLine.ProductId == nil && lineRequest.UnitPrice == nil {
			return nil, ErrPriceRequired
		}
		lineAmount := int64(pricedLine.Quantity) * pricedLine.UnitPrice
		if pricedLine.DiscountAmount > lineAmount {
			return nil, ErrDiscountTooHigh
		}
		pricedLine.LineTotal = lineAmount - pricedLine.DiscountAmount
		pricedLines = append(pricedLines, pricedLine)
	}
	return pricedLines, nil
}

func (service *BillService) Issue(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	if foundInvoice.Status != StatusDraft {
		return InvoiceView{}, ErrInvalidStatus
	}
	statusError := service.repository.SetStatus(ctx, querier, principal.CompanyId, invoiceId, StatusIssued, time.Now().UTC())
	if statusError != nil {
		return InvoiceView{}, statusError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) Accept(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	isOffer := foundInvoice.Kind == KindQuotation || foundInvoice.Kind == KindProforma
	if !isOffer || foundInvoice.Status != StatusIssued {
		return InvoiceView{}, ErrInvalidStatus
	}
	statusError := service.repository.SetStatus(ctx, querier, principal.CompanyId, invoiceId, StatusAccepted, time.Now().UTC())
	if statusError != nil {
		return InvoiceView{}, statusError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) Deliver(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	isBill := foundInvoice.Kind == KindInvoice || foundInvoice.Kind == KindSupplierInvoice
	if !isBill {
		return InvoiceView{}, ErrNotABill
	}
	if foundInvoice.DeliveredAt != nil {
		return InvoiceView{}, ErrAlreadyDelivered
	}
	if foundInvoice.Status == StatusCancelled {
		return InvoiceView{}, ErrInvalidStatus
	}

	lineViews, linesError := service.repository.Lines(ctx, querier, principal.CompanyId, invoiceId)
	if linesError != nil {
		return InvoiceView{}, linesError
	}
	for _, lineView := range lineViews {
		if lineView.ProductId == nil {
			continue
		}
		moveError := service.moveStock(ctx, querier, principal, foundInvoice.ShopId, *lineView.ProductId, -lineView.Quantity, ReasonDelivered, foundInvoice.Number)
		if moveError != nil {
			return InvoiceView{}, moveError
		}
	}

	deliveredError := service.repository.MarkDelivered(ctx, querier, principal.CompanyId, invoiceId, principal.UserId, time.Now().UTC())
	if deliveredError != nil {
		return InvoiceView{}, deliveredError
	}
	postError := service.ledger.PostInvoiceById(ctx, querier, principal.CompanyId, invoiceId)
	if postError != nil {
		return InvoiceView{}, postError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) RecordPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, request PaymentRequest) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	isBill := foundInvoice.Kind == KindInvoice || foundInvoice.Kind == KindSupplierInvoice
	if !isBill {
		return InvoiceView{}, ErrNotABill
	}
	if foundInvoice.Status != StatusIssued {
		return InvoiceView{}, ErrInvalidStatus
	}
	if request.Amount > foundInvoice.BalanceDue {
		return InvoiceView{}, ErrPaymentTooHigh
	}

	newPayment := Payment{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		InvoiceId: invoiceId,
		Method:    request.Method,
		Amount:    request.Amount,
		CreatedBy: principal.UserId,
		CreatedAt: time.Now().UTC(),
	}
	if foundInvoice.Kind == KindSupplierInvoice {
		moneyError := service.recordSupplierMoney(ctx, querier, principal, foundInvoice, request, &newPayment)
		if moneyError != nil {
			return InvoiceView{}, moneyError
		}
	} else {
		isCustomerMethod := request.Method == "cash" || request.Method == "card" || request.Method == "mobile"
		if !isCustomerMethod {
			return InvoiceView{}, ErrWrongMethod
		}
		reference := foundInvoice.Number
		if trimmedReference := trimmedOrNil(request.Reference); trimmedReference != nil {
			reference += " · " + *trimmedReference
		}
		customerPayment, paymentError := service.customersService.RecordInvoicePayment(ctx, querier, principal, foundInvoice.Party.Id, customers.PaymentRequest{
			Amount:    request.Amount,
			Method:    request.Method,
			Reference: &reference,
		})
		if paymentError != nil {
			return InvoiceView{}, paymentError
		}
		newPayment.CustomerPaymentId = &customerPayment.Id
	}

	insertError := service.repository.InsertPayment(ctx, querier, newPayment)
	if insertError != nil {
		return InvoiceView{}, insertError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) recordSupplierMoney(ctx context.Context, querier database.Querier, principal *identity.Principal, foundInvoice *InvoiceView, request PaymentRequest, newPayment *Payment) error {
	if request.Method == MethodOffset {
		return nil
	}
	return ErrSupplierReceiptsLater
}

func (service *BillService) VoidPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, paymentId uuid.UUID, request ReasonRequest) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	voidReason := strings.TrimSpace(request.Reason)
	if voidReason == "" {
		return InvoiceView{}, ErrInvalidReason
	}
	foundPayment, isVoided, findError := service.repository.FindPayment(ctx, querier, principal.CompanyId, invoiceId, paymentId)
	if findError != nil {
		return InvoiceView{}, findError
	}
	if foundPayment == nil {
		return InvoiceView{}, ErrPaymentNotFound
	}
	for _, paymentView := range foundInvoice.Payments {
		if paymentView.Id == paymentId && paymentView.IsVoided {
			isVoided = true
		}
	}
	if isVoided {
		return InvoiceView{}, ErrPaymentAlreadyVoided
	}

	if foundPayment.CustomerPaymentId != nil {
		_, voidError := service.customersService.VoidPayment(ctx, querier, principal, foundInvoice.Party.Id, *foundPayment.CustomerPaymentId, customers.VoidRequest{Reason: voidReason})
		if voidError != nil {
			return InvoiceView{}, voidError
		}
	}
	if foundPayment.SupplierReceiptId != nil {
		receiptError := service.voidSupplierReceipt(ctx, querier, principal, *foundPayment.SupplierReceiptId, voidReason)
		if receiptError != nil {
			return InvoiceView{}, receiptError
		}
	}
	voidError := service.repository.VoidPayment(ctx, querier, principal.CompanyId, paymentId, principal.UserId, time.Now().UTC())
	if voidError != nil {
		return InvoiceView{}, voidError
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) voidSupplierReceipt(ctx context.Context, querier database.Querier, principal *identity.Principal, receiptId uuid.UUID, reason string) error {
	return ErrSupplierReceiptsLater
}

func (service *BillService) Convert(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, request ConvertRequest) (InvoiceView, error) {
	if principal.ShopId == nil {
		return InvoiceView{}, ErrNoActiveShop
	}
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	isOffer := foundInvoice.Kind == KindQuotation || foundInvoice.Kind == KindProforma
	canConvert := (isOffer && request.Kind == KindInvoice) || (foundInvoice.Kind == KindInvoice && request.Kind == KindDeliveryNote)
	if !canConvert || foundInvoice.Status == StatusCancelled || foundInvoice.Status == StatusDraft {
		return InvoiceView{}, ErrCannotConvert
	}

	lineRequests := []LineRequest{}
	for _, lineView := range foundInvoice.Lines {
		unitPrice := lineView.UnitPrice
		lineRequest := LineRequest{
			Description:    lineView.Description,
			Unit:           lineView.Unit,
			Quantity:       lineView.Quantity,
			UnitPrice:      &unitPrice,
			DiscountAmount: lineView.DiscountAmount,
		}
		if lineView.ProductId != nil {
			productIdText := lineView.ProductId.String()
			lineRequest.ProductId = &productIdText
		}
		lineRequests = append(lineRequests, lineRequest)
	}
	customerIdText := foundInvoice.Party.Id.String()
	convertedRequest := InvoiceRequest{
		Kind:       request.Kind,
		CustomerId: &customerIdText,
		Reference:  foundInvoice.Reference,
		Notes:      foundInvoice.Notes,
		Lines:      lineRequests,
	}
	if request.Kind == KindInvoice {
		convertedRequest.Terms = foundInvoice.Terms
	}
	if request.Kind == KindDeliveryNote {
		emptyTerms := ""
		convertedRequest.Terms = &emptyTerms
	}

	kindError := company.requireKind(request.Kind)
	if kindError != nil {
		return InvoiceView{}, kindError
	}
	createdAt := time.Now().UTC()
	convertedInvoice := Invoice{
		Id:              uuid.Must(uuid.NewV7()),
		CompanyId:       principal.CompanyId,
		ShopId:          foundInvoice.ShopId,
		Kind:            request.Kind,
		Status:          StatusDraft,
		SourceInvoiceId: &foundInvoice.Id,
		CreatedBy:       principal.UserId,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}
	pricedLines, fillError := service.fill(ctx, querier, principal, company, &convertedInvoice, convertedRequest)
	if fillError != nil {
		return InvoiceView{}, fillError
	}
	numberError := service.assignNumber(ctx, querier, company, &convertedInvoice)
	if numberError != nil {
		return InvoiceView{}, numberError
	}
	insertError := service.repository.Insert(ctx, querier, convertedInvoice, pricedLines)
	if database.IsUniqueViolation(insertError) {
		return InvoiceView{}, ErrNumberInFlight
	}
	if insertError != nil {
		return InvoiceView{}, insertError
	}
	if isOffer && foundInvoice.Status == StatusIssued {
		statusError := service.repository.SetStatus(ctx, querier, principal.CompanyId, invoiceId, StatusAccepted, createdAt)
		if statusError != nil {
			return InvoiceView{}, statusError
		}
	}
	return service.get(ctx, querier, principal, company, convertedInvoice.Id)
}

func (service *BillService) Cancel(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, request ReasonRequest) (InvoiceView, error) {
	company, foundInvoice, lockError := service.lockInvoice(ctx, querier, principal, invoiceId)
	if lockError != nil {
		return InvoiceView{}, lockError
	}
	if foundInvoice.Status == StatusCancelled {
		return InvoiceView{}, ErrInvalidStatus
	}
	cancelReason := strings.TrimSpace(request.Reason)
	if cancelReason == "" {
		return InvoiceView{}, ErrInvalidReason
	}
	if foundInvoice.PaidTotal > 0 {
		return InvoiceView{}, ErrHasPayments
	}

	if foundInvoice.DeliveredAt != nil {
		for _, lineView := range foundInvoice.Lines {
			if lineView.ProductId == nil {
				continue
			}
			moveError := service.moveStock(ctx, querier, principal, foundInvoice.ShopId, *lineView.ProductId, lineView.Quantity, ReasonCancelled, foundInvoice.Number)
			if moveError != nil {
				return InvoiceView{}, moveError
			}
		}
	}
	cancelError := service.repository.MarkCancelled(ctx, querier, principal.CompanyId, invoiceId, principal.UserId, cancelReason, time.Now().UTC())
	if cancelError != nil {
		return InvoiceView{}, cancelError
	}
	if foundInvoice.DeliveredAt != nil {
		reverseError := service.ledger.PostInvoiceCancelById(ctx, querier, principal.CompanyId, invoiceId)
		if reverseError != nil {
			return InvoiceView{}, reverseError
		}
	}
	return service.get(ctx, querier, principal, company, invoiceId)
}

func (service *BillService) moveStock(ctx context.Context, querier database.Querier, principal *identity.Principal, shopId uuid.UUID, productId uuid.UUID, change int, reason string, reference string) error {
	_, movementError := service.stockService.RecordMovement(ctx, querier, stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    shopId,
		ProductId: productId,
		Change:    change,
		Reason:    reason,
		Reference: &reference,
		UserId:    &principal.UserId,
	})
	return movementError
}

func (service *BillService) lockInvoice(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (companyContext, *InvoiceView, error) {
	company, companyError := service.loadCompany(ctx, querier, principal.CompanyId)
	if companyError != nil {
		return companyContext{}, nil, companyError
	}
	wasLocked, lockError := service.repository.Lock(ctx, querier, principal.CompanyId, invoiceId)
	if lockError != nil {
		return companyContext{}, nil, lockError
	}
	if !wasLocked {
		return companyContext{}, nil, ErrInvoiceNotFound
	}
	foundInvoice, findError := service.get(ctx, querier, principal, company, invoiceId)
	if findError != nil {
		return companyContext{}, nil, findError
	}
	kindError := company.requireKind(foundInvoice.Kind)
	if kindError != nil {
		return companyContext{}, nil, kindError
	}
	return company, &foundInvoice, nil
}

func mustDate(dateText string) time.Time {
	parsedDate, _ := time.Parse(time.DateOnly, dateText)
	return parsedDate
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
