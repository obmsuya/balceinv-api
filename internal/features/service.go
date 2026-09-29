package features

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
)

var (
	ErrNeedsCustomers = errors.New("turn on customers before credit sales or customer orders")
	ErrNeedsSuppliers = errors.New("turn on suppliers before purchase orders")
	ErrNeedsVatNumber = errors.New("add the VAT registration number (VRN) before turning VAT on")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) Get(ctx context.Context, querier database.Querier, principal *identity.Principal) (FeaturesView, error) {
	companyFeatures, findError := service.repository.Find(ctx, querier, principal.CompanyId)
	if findError != nil {
		return FeaturesView{}, findError
	}
	return ToView(companyFeatures), nil
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, request UpdateFeaturesRequest) (FeaturesView, error) {
	companyFeatures, findError := service.repository.Find(ctx, querier, principal.CompanyId)
	if findError != nil {
		return FeaturesView{}, findError
	}

	changedFeatures := applyChanges(companyFeatures, request)

	needsCustomers := changedFeatures.CreditSalesEnabled || changedFeatures.CustomerOrdersEnabled
	if needsCustomers && !changedFeatures.CustomersEnabled {
		return FeaturesView{}, ErrNeedsCustomers
	}
	if changedFeatures.PurchaseOrdersEnabled && !changedFeatures.SuppliersEnabled {
		return FeaturesView{}, ErrNeedsSuppliers
	}
	hasVatNumber := changedFeatures.VatNumber != nil && *changedFeatures.VatNumber != ""
	if changedFeatures.VatRegistered && !hasVatNumber {
		return FeaturesView{}, ErrNeedsVatNumber
	}

	updatedBy := principal.UserId
	changedFeatures.UpdatedBy = &updatedBy
	changedFeatures.UpdatedAt = time.Now().UTC()

	saveError := service.repository.Save(ctx, querier, changedFeatures)
	if saveError != nil {
		return FeaturesView{}, saveError
	}

	return ToView(changedFeatures), nil
}

func applyChanges(companyFeatures Features, request UpdateFeaturesRequest) Features {
	changedFeatures := companyFeatures
	if request.SuppliersEnabled != nil {
		changedFeatures.SuppliersEnabled = *request.SuppliersEnabled
	}
	if request.PurchaseOrdersEnabled != nil {
		changedFeatures.PurchaseOrdersEnabled = *request.PurchaseOrdersEnabled
	}
	if request.CustomersEnabled != nil {
		changedFeatures.CustomersEnabled = *request.CustomersEnabled
	}
	if request.CreditSalesEnabled != nil {
		changedFeatures.CreditSalesEnabled = *request.CreditSalesEnabled
	}
	if request.CustomerOrdersEnabled != nil {
		changedFeatures.CustomerOrdersEnabled = *request.CustomerOrdersEnabled
	}
	if request.AccountingMode != nil {
		changedFeatures.AccountingMode = *request.AccountingMode
	}
	if request.VatRegistered != nil {
		changedFeatures.VatRegistered = *request.VatRegistered
	}
	if request.VatNumber != nil {
		trimmedNumber := strings.TrimSpace(*request.VatNumber)
		changedFeatures.VatNumber = &trimmedNumber
		if trimmedNumber == "" {
			changedFeatures.VatNumber = nil
		}
	}
	return changedFeatures
}
