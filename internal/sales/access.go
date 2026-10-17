package sales

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/google/uuid"
)

func (repository *Repository) IsSaleVisible(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID, userId uuid.UUID, isOwner bool) (bool, error) {
	query := `
		SELECT COUNT(*) FROM sales s
		WHERE s.company_id = $1 AND s.id = $2
		  AND ($3 OR EXISTS (SELECT 1 FROM user_shops us WHERE us.company_id = s.company_id AND us.shop_id = s.shop_id AND us.user_id = $4))
	`

	visibleCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, saleId, isOwner, userId).Scan(&visibleCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check the sale's shop: %w", scanError)
	}
	return visibleCount > 0, nil
}

func (service *Service) ensureSaleVisible(ctx context.Context, querier database.Querier, principal *identity.Principal, saleId uuid.UUID) error {
	isVisible, visibleError := service.repository.IsSaleVisible(ctx, querier, principal.CompanyId, saleId, principal.UserId, principal.IsOwner)
	if visibleError != nil {
		return visibleError
	}
	if !isVisible {
		return ErrSaleNotFound
	}
	return nil
}

func (service *Service) View(ctx context.Context, querier database.Querier, principal *identity.Principal, saleId uuid.UUID) (SaleView, error) {
	visibleError := service.ensureSaleVisible(ctx, querier, principal, saleId)
	if visibleError != nil {
		return SaleView{}, visibleError
	}
	return service.Get(ctx, querier, principal.CompanyId, saleId)
}

func (service *Service) ViewReceipt(ctx context.Context, querier database.Querier, principal *identity.Principal, saleId uuid.UUID) (ReceiptView, error) {
	visibleError := service.ensureSaleVisible(ctx, querier, principal, saleId)
	if visibleError != nil {
		return ReceiptView{}, visibleError
	}
	return service.Receipt(ctx, querier, principal.CompanyId, saleId)
}
