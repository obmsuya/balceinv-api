package transfers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrNoActiveShop     = errors.New("choose a shop first")
	ErrSameShop         = errors.New("choose a different shop to send the stock to")
	ErrShopNotFound     = errors.New("shop not found or closed")
	ErrShopNotAssigned  = errors.New("you can only send stock from a shop you work in")
	ErrDuplicateProduct = errors.New("each product can appear only once in a transfer")
	ErrProductNotFound  = errors.New("one of the products was not found")
	ErrTransferNotFound = errors.New("transfer not found")
)

type Service struct {
	repository   *Repository
	stockService *stock.Service
	ledger       *accounting.Ledger
}

func NewService(repository *Repository, stockService *stock.Service, ledger *accounting.Ledger) *Service {
	return &Service{
		repository:   repository,
		stockService: stockService,
		ledger:       ledger,
	}
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request TransferRequest) (TransferView, error) {
	fromShopId, toShopId, shopError := service.checkShops(ctx, querier, principal, request)
	if shopError != nil {
		return TransferView{}, shopError
	}

	productIds, quantities, itemError := readItems(request.Items)
	if itemError != nil {
		return TransferView{}, itemError
	}

	activeProductCount, countError := service.repository.CountActiveProducts(ctx, querier, principal.CompanyId, productIds)
	if countError != nil {
		return TransferView{}, countError
	}
	if activeProductCount != len(productIds) {
		return TransferView{}, ErrProductNotFound
	}

	newTransfer := Transfer{
		Id:         uuid.Must(uuid.NewV7()),
		CompanyId:  principal.CompanyId,
		FromShopId: fromShopId,
		ToShopId:   toShopId,
		Note:       trimmedOrNil(request.Note),
		UserId:     &principal.UserId,
		CreatedAt:  time.Now().UTC(),
	}
	newItems := make([]TransferItem, 0, len(productIds))
	for itemIndex, productId := range productIds {
		newItems = append(newItems, TransferItem{
			CompanyId:  principal.CompanyId,
			TransferId: newTransfer.Id,
			ProductId:  productId,
			Quantity:   quantities[itemIndex],
		})
	}

	insertError := service.repository.Insert(ctx, querier, newTransfer, newItems)
	if insertError != nil {
		return TransferView{}, insertError
	}

	transferReference := newTransfer.Id.String()
	for _, newItem := range newItems {
		moveError := service.moveItem(ctx, querier, principal, newTransfer, newItem, transferReference)
		if moveError != nil {
			return TransferView{}, moveError
		}
	}
	postError := service.ledger.PostTransferById(ctx, querier, principal.CompanyId, newTransfer.Id)
	if postError != nil {
		return TransferView{}, postError
	}

	return service.Get(ctx, querier, principal, newTransfer.Id)
}

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, limit int, offset int) (response.Page[TransferView], error) {
	if principal.ShopId == nil {
		return response.Page[TransferView]{}, ErrNoActiveShop
	}

	totalTransfers, countError := service.repository.CountForShop(ctx, querier, principal.CompanyId, *principal.ShopId)
	if countError != nil {
		return response.Page[TransferView]{}, countError
	}

	transferViews, listError := service.repository.ListForShop(ctx, querier, principal.CompanyId, *principal.ShopId, limit, offset)
	if listError != nil {
		return response.Page[TransferView]{}, listError
	}

	transferPage := response.Page[TransferView]{
		Items:  transferViews,
		Total:  totalTransfers,
		Limit:  limit,
		Offset: offset,
	}
	return transferPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, principal *identity.Principal, transferId uuid.UUID) (TransferView, error) {
	foundTransfer, findError := service.repository.Find(ctx, querier, principal.CompanyId, transferId)
	if findError != nil {
		return TransferView{}, findError
	}
	if foundTransfer == nil {
		return TransferView{}, ErrTransferNotFound
	}

	transferItems, itemsError := service.repository.ListItems(ctx, querier, principal.CompanyId, transferId)
	if itemsError != nil {
		return TransferView{}, itemsError
	}
	foundTransfer.Items = transferItems

	return *foundTransfer, nil
}

func (service *Service) checkShops(ctx context.Context, querier database.Querier, principal *identity.Principal, request TransferRequest) (uuid.UUID, uuid.UUID, error) {
	fromShopId := principal.ShopId
	if request.FromShopId != nil {
		requestedShopId := uuid.MustParse(*request.FromShopId)
		fromShopId = &requestedShopId
	}
	if fromShopId == nil {
		return uuid.Nil, uuid.Nil, ErrNoActiveShop
	}

	toShopId := uuid.MustParse(request.ToShopId)
	if toShopId == *fromShopId {
		return uuid.Nil, uuid.Nil, ErrSameShop
	}

	for _, shopId := range []uuid.UUID{*fromShopId, toShopId} {
		isOpenShop, findError := service.repository.FindOpenShop(ctx, querier, principal.CompanyId, shopId)
		if findError != nil {
			return uuid.Nil, uuid.Nil, findError
		}
		if !isOpenShop {
			return uuid.Nil, uuid.Nil, ErrShopNotFound
		}
	}

	if !principal.IsOwner {
		isAssigned, assignedError := service.repository.IsAssignedToShop(ctx, querier, principal.CompanyId, principal.UserId, *fromShopId)
		if assignedError != nil {
			return uuid.Nil, uuid.Nil, assignedError
		}
		if !isAssigned {
			return uuid.Nil, uuid.Nil, ErrShopNotAssigned
		}
	}

	return *fromShopId, toShopId, nil
}

func (service *Service) moveItem(ctx context.Context, querier database.Querier, principal *identity.Principal, transfer Transfer, item TransferItem, reference string) error {
	outgoingMovement := stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    transfer.FromShopId,
		ProductId: item.ProductId,
		Change:    -item.Quantity,
		Reason:    "transfer_out",
		Reference: &reference,
		UserId:    &principal.UserId,
	}
	_, outgoingError := service.stockService.RecordMovement(ctx, querier, outgoingMovement)
	if outgoingError != nil {
		return outgoingError
	}

	incomingMovement := stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    transfer.ToShopId,
		ProductId: item.ProductId,
		Change:    item.Quantity,
		Reason:    "transfer_in",
		Reference: &reference,
		UserId:    &principal.UserId,
	}
	_, incomingError := service.stockService.RecordMovement(ctx, querier, incomingMovement)
	return incomingError
}

func readItems(itemRequests []TransferItemRequest) ([]uuid.UUID, []int, error) {
	productIds := make([]uuid.UUID, 0, len(itemRequests))
	quantities := make([]int, 0, len(itemRequests))
	seenProducts := map[uuid.UUID]bool{}

	for _, itemRequest := range itemRequests {
		productId := uuid.MustParse(itemRequest.ProductId)
		if seenProducts[productId] {
			return nil, nil, ErrDuplicateProduct
		}
		seenProducts[productId] = true
		productIds = append(productIds, productId)
		quantities = append(quantities, itemRequest.Quantity)
	}

	return productIds, quantities, nil
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
