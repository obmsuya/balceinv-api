package products

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

const (
	MaximumImageBytes         = 2 << 20
	maximumMetadataKeys       = 50
	maximumMetadataKeyLength  = 60
	maximumMetadataTextLength = 500
	defaultWholesaleMinimum   = 10
	defaultUnit               = "pcs"
)

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrParentNotFound       = errors.New("the parent product was not found")
	ErrNestedVariant        = errors.New("a variant cannot have its own variants")
	ErrVariantLabelRequired = errors.New("a variant needs a label such as 500ml or Red")
	ErrSkuTaken             = errors.New("another product already uses this SKU")
	ErrBarcodeTaken         = errors.New("another product already uses one of these barcodes")
	ErrDuplicateBarcode     = errors.New("the same barcode is listed twice")
	ErrInvalidMetadata      = errors.New("metadata must be up to 50 short text, number or yes/no values")
	ErrNoActiveShop         = errors.New("choose a shop before adding opening stock")
	ErrAddonNotFound        = errors.New("add-on not found")
	ErrAddonNameTaken       = errors.New("this product already has an add-on with that name")
	ErrSupplierNotFound     = errors.New("the usual supplier was not found or is turned off")
)

type Service struct {
	repository   *Repository
	stockService *stock.Service
	objectStore  storage.Store
}

func NewService(repository *Repository, stockService *stock.Service, objectStore storage.Store) *Service {
	return &Service{
		repository:   repository,
		stockService: stockService,
		objectStore:  objectStore,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, filter ListFilter, limit int, offset int) (response.Page[ProductView], error) {
	emptyPage := response.Page[ProductView]{}

	totalProducts, countError := service.repository.Count(ctx, querier, principal.CompanyId, filter)
	if countError != nil {
		return emptyPage, countError
	}

	pageProducts, listError := service.repository.List(ctx, querier, principal.CompanyId, principal.ShopId, filter, limit, offset)
	if listError != nil {
		return emptyPage, listError
	}

	productViews, viewError := service.withBarcodes(ctx, querier, principal.CompanyId, pageProducts)
	if viewError != nil {
		return emptyPage, viewError
	}

	productPage := response.Page[ProductView]{
		Items:  productViews,
		Total:  totalProducts,
		Limit:  limit,
		Offset: offset,
	}

	return productPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID) (ProductView, error) {
	foundProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, principal.ShopId, productId)
	if findError != nil {
		return ProductView{}, findError
	}
	if foundProduct == nil {
		return ProductView{}, ErrProductNotFound
	}

	productViews, viewError := service.withBarcodes(ctx, querier, principal.CompanyId, []Product{*foundProduct})
	if viewError != nil {
		return ProductView{}, viewError
	}

	return productViews[0], nil
}

func (service *Service) Lookup(ctx context.Context, querier database.Querier, principal *identity.Principal, code string) (LookupView, error) {
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return LookupView{}, ErrProductNotFound
	}

	productId, packSize, findError := service.repository.FindIdByCode(ctx, querier, principal.CompanyId, trimmedCode)
	if findError != nil {
		return LookupView{}, findError
	}
	if productId == nil {
		return LookupView{}, ErrProductNotFound
	}

	productView, getError := service.Get(ctx, querier, principal, *productId)
	if getError != nil {
		return LookupView{}, getError
	}

	lookupView := LookupView{
		Product:  productView,
		PackSize: packSize,
	}
	return lookupView, nil
}

func (service *Service) ListVariants(ctx context.Context, querier database.Querier, principal *identity.Principal, parentId uuid.UUID) ([]ProductView, error) {
	parentProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, principal.ShopId, parentId)
	if findError != nil {
		return nil, findError
	}
	if parentProduct == nil {
		return nil, ErrProductNotFound
	}

	variantProducts, listError := service.repository.ListVariants(ctx, querier, principal.CompanyId, principal.ShopId, parentId)
	if listError != nil {
		return nil, listError
	}

	return service.withBarcodes(ctx, querier, principal.CompanyId, variantProducts)
}

func (service *Service) ListCategories(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]string, error) {
	return service.repository.ListCategories(ctx, querier, companyId)
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request CreateProductRequest) (ProductView, error) {
	createdProductId, createError := service.createProduct(ctx, querier, principal, request)
	if createError != nil {
		return ProductView{}, createError
	}
	return service.Get(ctx, querier, principal, createdProductId)
}

func (service *Service) createProduct(ctx context.Context, querier database.Querier, principal *identity.Principal, request CreateProductRequest) (uuid.UUID, error) {
	var parentId *uuid.UUID
	hasParent := request.ParentId != nil
	if hasParent {
		parsedParentId := uuid.MustParse(*request.ParentId)
		parentCheckError := service.checkParent(ctx, querier, principal, parsedParentId, request.VariantLabel)
		if parentCheckError != nil {
			return uuid.Nil, parentCheckError
		}
		parentId = &parsedParentId
	}

	hasOpeningStock := request.OpeningQuantity > 0
	hasActiveShop := principal.ShopId != nil
	if hasOpeningStock && !hasActiveShop {
		return uuid.Nil, ErrNoActiveShop
	}

	createdAt := time.Now().UTC()
	newProduct, buildError := buildProduct(Product{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		ParentId:  parentId,
		IsActive:  true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}, request.ProductFields)
	if buildError != nil {
		return uuid.Nil, buildError
	}
	preferredSupplierId, supplierError := service.resolvePreferredSupplier(ctx, querier, principal.CompanyId, request.PreferredSupplierId, nil)
	if supplierError != nil {
		return uuid.Nil, supplierError
	}
	newProduct.PreferredSupplierId = preferredSupplierId

	insertError := service.repository.Insert(ctx, querier, newProduct)
	if insertError != nil {
		if database.IsUniqueViolation(insertError) {
			return uuid.Nil, ErrSkuTaken
		}
		return uuid.Nil, insertError
	}

	barcodeError := service.saveBarcodes(ctx, querier, principal.CompanyId, newProduct.Id, request.Barcodes)
	if barcodeError != nil {
		return uuid.Nil, barcodeError
	}

	if hasActiveShop {
		stockError := service.prepareStock(ctx, querier, principal, newProduct.Id, request.MinStock, request.OpeningQuantity)
		if stockError != nil {
			return uuid.Nil, stockError
		}
	}

	return newProduct.Id, nil
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID, request UpdateProductRequest) (ProductView, error) {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, principal.ShopId, productId)
	if findError != nil {
		return ProductView{}, findError
	}
	if existingProduct == nil {
		return ProductView{}, ErrProductNotFound
	}

	isVariant := existingProduct.ParentId != nil
	missingVariantLabel := isVariant && strings.TrimSpace(request.VariantLabel) == ""
	if missingVariantLabel {
		return ProductView{}, ErrVariantLabelRequired
	}

	changedProduct, buildError := buildProduct(*existingProduct, request.ProductFields)
	if buildError != nil {
		return ProductView{}, buildError
	}
	if request.IsActive != nil {
		changedProduct.IsActive = *request.IsActive
	}
	preferredSupplierId, supplierError := service.resolvePreferredSupplier(ctx, querier, principal.CompanyId, request.PreferredSupplierId, existingProduct.PreferredSupplierId)
	if supplierError != nil {
		return ProductView{}, supplierError
	}
	changedProduct.PreferredSupplierId = preferredSupplierId

	updateError := service.repository.Update(ctx, querier, changedProduct)
	if updateError != nil {
		if database.IsUniqueViolation(updateError) {
			return ProductView{}, ErrSkuTaken
		}
		return ProductView{}, updateError
	}

	priceChanged := existingProduct.Price != changedProduct.Price
	if priceChanged {
		priceChange := PriceChange{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: principal.CompanyId,
			ProductId: productId,
			OldPrice:  existingProduct.Price,
			NewPrice:  changedProduct.Price,
			ChangedBy: &principal.UserId,
			CreatedAt: time.Now().UTC(),
		}
		priceHistoryError := service.repository.InsertPriceChange(ctx, querier, priceChange)
		if priceHistoryError != nil {
			return ProductView{}, priceHistoryError
		}
	}

	hasBarcodeChanges := request.Barcodes != nil
	if hasBarcodeChanges {
		barcodeError := service.saveBarcodes(ctx, querier, principal.CompanyId, productId, request.Barcodes)
		if barcodeError != nil {
			return ProductView{}, barcodeError
		}
	}

	hasMinimumStockChange := request.MinStock != nil && principal.ShopId != nil
	if hasMinimumStockChange {
		stockError := service.prepareStock(ctx, querier, principal, productId, request.MinStock, 0)
		if stockError != nil {
			return ProductView{}, stockError
		}
	}

	return service.Get(ctx, querier, principal, productId)
}

func (service *Service) Archive(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID) error {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, productId)
	if findError != nil {
		return findError
	}
	if existingProduct == nil {
		return ErrProductNotFound
	}

	return service.repository.SetActive(ctx, querier, principal.CompanyId, productId, false)
}

func (service *Service) Restore(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID) (ProductView, error) {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, productId)
	if findError != nil {
		return ProductView{}, findError
	}
	if existingProduct == nil {
		return ProductView{}, ErrProductNotFound
	}

	restoreError := service.repository.SetActive(ctx, querier, principal.CompanyId, productId, true)
	if restoreError != nil {
		return ProductView{}, restoreError
	}

	return service.Get(ctx, querier, principal, productId)
}

func (service *Service) UploadImage(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID, imageBytes []byte) (ProductView, error) {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, productId)
	if findError != nil {
		return ProductView{}, findError
	}
	if existingProduct == nil {
		return ProductView{}, ErrProductNotFound
	}

	imageKey, storeError := media.StoreImage(ctx, service.objectStore, media.FolderProducts, principal.CompanyId, imageBytes, MaximumImageBytes)
	if storeError != nil {
		return ProductView{}, storeError
	}

	setImageError := service.repository.SetImageKey(ctx, querier, principal.CompanyId, productId, imageKey)
	if setImageError != nil {
		return ProductView{}, setImageError
	}

	return service.Get(ctx, querier, principal, productId)
}

func (service *Service) resolvePreferredSupplier(ctx context.Context, querier database.Querier, companyId uuid.UUID, requestedSupplierId *string, currentSupplierId *uuid.UUID) (*uuid.UUID, error) {
	if requestedSupplierId == nil {
		return currentSupplierId, nil
	}
	trimmedSupplierId := strings.TrimSpace(*requestedSupplierId)
	if trimmedSupplierId == "" {
		return nil, nil
	}
	supplierId, parseError := uuid.Parse(trimmedSupplierId)
	if parseError != nil {
		return nil, ErrSupplierNotFound
	}
	isUnchanged := currentSupplierId != nil && *currentSupplierId == supplierId
	if isUnchanged {
		return currentSupplierId, nil
	}

	isActiveSupplier, findError := service.repository.IsActiveSupplier(ctx, querier, companyId, supplierId)
	if findError != nil {
		return nil, findError
	}
	if !isActiveSupplier {
		return nil, ErrSupplierNotFound
	}
	return &supplierId, nil
}

func (service *Service) checkParent(ctx context.Context, querier database.Querier, principal *identity.Principal, parentId uuid.UUID, variantLabel string) error {
	parentProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, parentId)
	if findError != nil {
		return findError
	}
	if parentProduct == nil {
		return ErrParentNotFound
	}
	if parentProduct.ParentId != nil {
		return ErrNestedVariant
	}
	if strings.TrimSpace(variantLabel) == "" {
		return ErrVariantLabelRequired
	}
	return nil
}

func (service *Service) saveBarcodes(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, barcodeInputs []BarcodeInput) error {
	seenCodes := map[string]bool{}
	newBarcodes := make([]Barcode, 0, len(barcodeInputs))
	createdAt := time.Now().UTC()

	for _, barcodeInput := range barcodeInputs {
		barcodeCode := strings.TrimSpace(barcodeInput.Code)
		if seenCodes[barcodeCode] {
			return ErrDuplicateBarcode
		}
		seenCodes[barcodeCode] = true

		packSize := barcodeInput.PackSize
		if packSize == 0 {
			packSize = 1
		}

		newBarcodes = append(newBarcodes, Barcode{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: companyId,
			ProductId: productId,
			Code:      barcodeCode,
			PackSize:  packSize,
			CreatedAt: createdAt,
		})
	}

	replaceError := service.repository.ReplaceBarcodes(ctx, querier, companyId, productId, newBarcodes)
	if replaceError != nil {
		if database.IsUniqueViolation(replaceError) {
			return ErrBarcodeTaken
		}
		return replaceError
	}

	return nil
}

func (service *Service) prepareStock(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID, minimumStock *int, openingQuantity int) error {
	shopId := *principal.ShopId
	chosenMinimum := stock.DefaultMinimumStock
	if minimumStock != nil {
		chosenMinimum = *minimumStock
	}

	stockRepository := stock.NewRepository()
	ensureError := stockRepository.EnsureShopStock(ctx, querier, principal.CompanyId, shopId, productId, chosenMinimum)
	if ensureError != nil {
		return ensureError
	}
	if minimumStock != nil {
		setMinimumError := stockRepository.SetMinimumStock(ctx, querier, principal.CompanyId, shopId, productId, chosenMinimum)
		if setMinimumError != nil {
			return setMinimumError
		}
	}

	hasOpeningStock := openingQuantity > 0
	if !hasOpeningStock {
		return nil
	}

	openingReference := "Opening stock"
	_, movementError := service.stockService.RecordAndBookMovement(ctx, querier, stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    shopId,
		ProductId: productId,
		Change:    openingQuantity,
		Reason:    "opening",
		Reference: &openingReference,
		UserId:    &principal.UserId,
	})
	return movementError
}

func (service *Service) withBarcodes(ctx context.Context, querier database.Querier, companyId uuid.UUID, foundProducts []Product) ([]ProductView, error) {
	productIds := make([]uuid.UUID, 0, len(foundProducts))
	for _, foundProduct := range foundProducts {
		productIds = append(productIds, foundProduct.Id)
	}

	barcodesByProduct, barcodeError := service.repository.ListBarcodes(ctx, querier, companyId, productIds)
	if barcodeError != nil {
		return nil, barcodeError
	}

	productViews := make([]ProductView, 0, len(foundProducts))
	for _, foundProduct := range foundProducts {
		productViews = append(productViews, toProductView(foundProduct, barcodesByProduct[foundProduct.Id]))
	}
	return productViews, nil
}
