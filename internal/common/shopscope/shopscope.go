package shopscope

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/google/uuid"
)

const AllShops = "all"

var (
	ErrNoActiveShop    = errors.New("choose a shop first")
	ErrShopNotAssigned = errors.New("you can only see shops you work in")
	ErrShopNotFound    = errors.New("shop not found")
)

type Scope struct {
	AllShops bool
	ShopIds  []uuid.UUID
}

func (scope Scope) IsSingleShop() bool {
	return !scope.AllShops && len(scope.ShopIds) == 1
}

func (scope Scope) Condition(column string, arguments *[]any) string {
	if scope.AllShops {
		return ""
	}
	placeholders := make([]string, 0, len(scope.ShopIds))
	for _, shopId := range scope.ShopIds {
		*arguments = append(*arguments, shopId)
		placeholders = append(placeholders, "$"+strconv.Itoa(len(*arguments)))
	}
	return " AND " + column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func Resolve(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string) (Scope, error) {
	if requestedShop == "" {
		if principal.ShopId == nil {
			return Scope{}, ErrNoActiveShop
		}
		return Scope{ShopIds: []uuid.UUID{*principal.ShopId}}, nil
	}

	if requestedShop == AllShops {
		if principal.IsOwner {
			return Scope{AllShops: true}, nil
		}
		assignedShopIds, assignedError := assignedShops(ctx, querier, principal.CompanyId, principal.UserId)
		if assignedError != nil {
			return Scope{}, assignedError
		}
		if len(assignedShopIds) == 0 {
			return Scope{}, ErrShopNotAssigned
		}
		return Scope{ShopIds: assignedShopIds}, nil
	}

	shopId, parseError := uuid.Parse(requestedShop)
	if parseError != nil {
		return Scope{}, ErrShopNotFound
	}
	shopExists, existsError := shopExists(ctx, querier, principal.CompanyId, shopId)
	if existsError != nil {
		return Scope{}, existsError
	}
	if !shopExists {
		return Scope{}, ErrShopNotFound
	}
	if principal.IsOwner {
		return Scope{ShopIds: []uuid.UUID{shopId}}, nil
	}

	assignedShopIds, assignedError := assignedShops(ctx, querier, principal.CompanyId, principal.UserId)
	if assignedError != nil {
		return Scope{}, assignedError
	}
	for _, assignedShopId := range assignedShopIds {
		if assignedShopId == shopId {
			return Scope{ShopIds: []uuid.UUID{shopId}}, nil
		}
	}
	return Scope{}, ErrShopNotAssigned
}

func assignedShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT shop_id FROM user_shops WHERE company_id = $1 AND user_id = $2 ORDER BY shop_id`

	shopRows, queryError := querier.QueryContext(ctx, query, companyId, userId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list assigned shops: %w", queryError)
	}
	defer shopRows.Close()

	shopIds := []uuid.UUID{}
	for shopRows.Next() {
		shopId := uuid.UUID{}
		scanError := shopRows.Scan(&shopId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an assigned shop: %w", scanError)
		}
		shopIds = append(shopIds, shopId)
	}
	rowsError := shopRows.Err()
	if rowsError != nil {
		return nil, fmt.Errorf("failed to list assigned shops: %w", rowsError)
	}
	return shopIds, nil
}

func shopExists(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	shopCount := 0
	scanError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND id = $2`, companyId, shopId).Scan(&shopCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find the shop: %w", scanError)
	}
	return shopCount == 1, nil
}
