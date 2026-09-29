package sales

import (
	"context"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/google/uuid"
)

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, filter SaleFilter, limit int, offset int) (response.Page[SaleSummaryView], error) {
	if principal.ShopId == nil {
		return response.Page[SaleSummaryView]{}, ErrNoActiveShop
	}

	totals, totalsError := service.repository.Totals(ctx, querier, principal.CompanyId, *principal.ShopId, filter)
	if totalsError != nil {
		return response.Page[SaleSummaryView]{}, totalsError
	}

	summaries, listError := service.repository.ListSummaries(ctx, querier, principal.CompanyId, *principal.ShopId, filter, limit, offset)
	if listError != nil {
		return response.Page[SaleSummaryView]{}, listError
	}

	salePage := response.Page[SaleSummaryView]{
		Items:  summaries,
		Total:  totals.SaleCount,
		Limit:  limit,
		Offset: offset,
	}
	return salePage, nil
}

func (service *Service) Totals(ctx context.Context, querier database.Querier, principal *identity.Principal, filter SaleFilter) (TotalsView, error) {
	if principal.ShopId == nil {
		return TotalsView{}, ErrNoActiveShop
	}
	return service.repository.Totals(ctx, querier, principal.CompanyId, *principal.ShopId, filter)
}

func (service *Service) Receipt(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) (ReceiptView, error) {
	saleView, saleError := service.Get(ctx, querier, companyId, saleId)
	if saleError != nil {
		return ReceiptView{}, saleError
	}

	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, querier, companyId)
	if profileError != nil {
		return ReceiptView{}, profileError
	}
	companySettings, settingsError := service.findSettings(ctx, querier, companyId)
	if settingsError != nil {
		return ReceiptView{}, settingsError
	}
	if companyProfile == nil {
		return ReceiptView{}, ErrMissingSettings
	}

	shopView, shopError := service.repository.FindReceiptShop(ctx, querier, companyId, saleView.ShopId)
	if shopError != nil {
		return ReceiptView{}, shopError
	}

	receiptView := ReceiptView{
		Sale: saleView,
		Company: ReceiptCompanyView{
			Name:          companyProfile.Name,
			Address:       companyProfile.Address,
			Phone:         companyProfile.Phone,
			Tin:           companyProfile.Tin,
			LogoUrl:       media.PublicUrl(companyProfile.LogoKey),
			ReceiptHeader: companyProfile.ReceiptHeader,
			ReceiptFooter: companyProfile.ReceiptFooter,
		},
		Shop:             shopView,
		ShowTax:          companySettings.ShowTaxOnReceipt,
		ShowBarcodes:     companySettings.ShowBarcodesOnReceipt,
		ReceiptLanguage:  companySettings.ReceiptLanguage,
		PaperWidthMillis: companySettings.PrinterPaperWidth,
	}
	return receiptView, nil
}
