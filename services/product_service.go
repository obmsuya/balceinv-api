package services

import (
	"errors"
	"fmt"
	"mime/multipart"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type ProductService struct {
	productRepository *repository.ProductRepository
}

func NewProductService(productRepository *repository.ProductRepository) *ProductService {
	return &ProductService{productRepository: productRepository}
}

type CreateProductInput struct {
	Name           string         `json:"name"`
	SKU            string         `json:"sku"`
	Barcode        *string        `json:"barcode"`
	ParentID       *uint          `json:"parent_id"`
	VariantLabel   string         `json:"variant_label"`
	Price          float64        `json:"price"`
	CostPrice      float64        `json:"cost_price"`
	Quantity       int            `json:"quantity"`
	MinStock       int            `json:"min_stock"`
	WholesalePrice *float64       `json:"wholesale_price"`
	WholesaleMin   int            `json:"wholesale_min"`
	Category       *string        `json:"category"`
	Unit           string         `json:"unit"`
	PiecesPerUnit  int            `json:"pieces_per_unit"`
	Image          *string        `json:"image"`
	Metadata       models.JSONMap `json:"metadata"`
}

type UpdateProductInput struct {
	Name           string         `json:"name"`
	VariantLabel   string         `json:"variant_label"`
	Price          float64        `json:"price"`
	CostPrice      float64        `json:"cost_price"`
	MinStock       int            `json:"min_stock"`
	WholesalePrice *float64       `json:"wholesale_price"`
	WholesaleMin   int            `json:"wholesale_min"`
	Category       *string        `json:"category"`
	Unit           string         `json:"unit"`
	PiecesPerUnit  int            `json:"pieces_per_unit"`
	Image          *string        `json:"image"`
	Metadata       models.JSONMap `json:"metadata"`
}

type UploadResult struct {
	Created int                 `json:"created"`
	Errors  []map[string]string `json:"errors"`
}

func (service *ProductService) GetAll(search, category string) ([]models.Product, error) {
	return service.productRepository.FindAll(search, category)
}

func (service *ProductService) GetByID(id uint) (*models.Product, error) {
	product, err := service.productRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	return product, err
}

func (service *ProductService) GetVariants(parentID uint) ([]models.Product, error) {
	_, err := service.productRepository.FindByID(parentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	return service.productRepository.FindVariantsByParentID(parentID)
}

func (service *ProductService) Create(input CreateProductInput) (*models.Product, error) {
	existingProduct, err := service.productRepository.FindBySKU(input.SKU)
	if err == nil && existingProduct != nil {
		return nil, errors.New("product with this SKU already exists")
	}

	if input.ParentID != nil {
		_, err := service.productRepository.FindByID(*input.ParentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("parent product not found")
		}
		if err != nil {
			return nil, err
		}
		if input.VariantLabel == "" {
			return nil, errors.New("variant label is required when parent_id is set")
		}
	}

	unit := input.Unit
	if unit == "" {
		unit = "pcs"
	}

	piecesPerUnit := input.PiecesPerUnit
	if piecesPerUnit == 0 {
		piecesPerUnit = 1
	}

	minStock := input.MinStock
	if minStock == 0 {
		minStock = 5
	}

	wholesaleMin := input.WholesaleMin
	if wholesaleMin == 0 {
		wholesaleMin = 10
	}

	product := &models.Product{
		Name:           input.Name,
		SKU:            input.SKU,
		Barcode:        input.Barcode,
		ParentID:       input.ParentID,
		VariantLabel:   input.VariantLabel,
		Price:          input.Price,
		CostPrice:      input.CostPrice,
		Quantity:       input.Quantity,
		MinStock:       minStock,
		WholesalePrice: input.WholesalePrice,
		WholesaleMin:   wholesaleMin,
		Category:       input.Category,
		Unit:           unit,
		PiecesPerUnit:  piecesPerUnit,
		Image:          input.Image,
		Metadata:       input.Metadata,
	}

	if err := service.productRepository.Create(product); err != nil {
		return nil, err
	}

	if input.Quantity > 0 {
		reference := "Initial stock"
		service.productRepository.CreateStockMovement(&models.StockMovement{
			ProductID:   product.ID,
			Change:      input.Quantity,
			NewQuantity: input.Quantity,
			Reason:      "adjust",
			Reference:   &reference,
		})
	}

	return product, nil
}

func (service *ProductService) Update(id uint, input UpdateProductInput, userID *uint) (*models.Product, error) {
	product, err := service.productRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	if input.Price != 0 && input.Price != product.Price {
		oldPrice := product.Price
		newPrice := input.Price
		service.productRepository.CreatePriceHistory(&models.PriceHistory{
			ProductID: id,
			OldPrice:  &oldPrice,
			NewPrice:  &newPrice,
			UserID:    userID,
		})
	}

	product.Name = input.Name
	product.VariantLabel = input.VariantLabel
	product.Price = input.Price
	product.CostPrice = input.CostPrice
	product.MinStock = input.MinStock
	product.WholesalePrice = input.WholesalePrice
	product.WholesaleMin = input.WholesaleMin
	product.Category = input.Category
	product.Unit = input.Unit
	product.PiecesPerUnit = input.PiecesPerUnit
	product.Metadata = input.Metadata

	if input.Image != nil {
		product.Image = input.Image
	}

	if err := service.productRepository.Update(product); err != nil {
		return nil, err
	}

	return product, nil
}

func (service *ProductService) UpdateImage(id uint, imageDataURI string) (*models.Product, error) {
	product, err := service.productRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	product.Image = &imageDataURI

	if err := service.productRepository.Update(product); err != nil {
		return nil, err
	}

	return product, nil
}

func (service *ProductService) Delete(id uint) error {
	_, err := service.productRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("product not found")
	}
	return service.productRepository.Delete(id)
}

func (service *ProductService) GetLowStock() ([]models.Product, error) {
	return service.productRepository.FindLowStock()
}

func (service *ProductService) UploadExcel(fileHeader *multipart.FileHeader) (*UploadResult, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return nil, errors.New("could not open uploaded file")
	}
	defer file.Close()

	spreadsheet, err := excelize.OpenReader(file)
	if err != nil {
		return nil, errors.New("could not parse Excel file")
	}

	rows, err := spreadsheet.GetRows(spreadsheet.GetSheetName(0))
	if err != nil {
		return nil, errors.New("could not read sheet")
	}

	result := &UploadResult{Errors: []map[string]string{}}

	if len(rows) < 2 {
		return result, nil
	}

	headers := rows[0]
	columnIndexByName := map[string]int{}
	for index, header := range headers {
		columnIndexByName[header] = index
	}

	getColumnValue := func(row []string, columnName string) string {
		columnIndex, exists := columnIndexByName[columnName]
		if !exists || columnIndex >= len(row) {
			return ""
		}
		return row[columnIndex]
	}

	for rowNumber, row := range rows[1:] {
		sku := getColumnValue(row, "sku")
		if sku == "" {
			result.Errors = append(result.Errors, map[string]string{
				"row": fmt.Sprintf("%d", rowNumber+2), "error": "missing sku",
			})
			continue
		}

		input := CreateProductInput{
			Name:      getColumnValue(row, "name"),
			SKU:       sku,
			Price:     parseFloat(getColumnValue(row, "price")),
			CostPrice: parseFloat(getColumnValue(row, "costPrice")),
			Quantity:  parseInt(getColumnValue(row, "quantity")),
			MinStock:  parseInt(getColumnValue(row, "minStock")),
			Unit:      getColumnValue(row, "unit"),
		}

		if barcodeValue := getColumnValue(row, "barcode"); barcodeValue != "" {
			input.Barcode = &barcodeValue
		}
		if categoryValue := getColumnValue(row, "category"); categoryValue != "" {
			input.Category = &categoryValue
		}
		if wholesalePriceValue := parseFloat(getColumnValue(row, "wholesalePrice")); wholesalePriceValue > 0 {
			input.WholesalePrice = &wholesalePriceValue
		}
		if wholesaleMinValue := parseInt(getColumnValue(row, "wholesaleMin")); wholesaleMinValue > 0 {
			input.WholesaleMin = wholesaleMinValue
		}
		if piecesPerUnitValue := parseInt(getColumnValue(row, "piecesPerUnit")); piecesPerUnitValue > 0 {
			input.PiecesPerUnit = piecesPerUnitValue
		}

		if _, err := service.Create(input); err != nil {
			result.Errors = append(result.Errors, map[string]string{
				"sku": sku, "error": err.Error(),
			})
			continue
		}

		result.Created++
	}

	return result, nil
}

func (service *ProductService) GetTemplate() ([]byte, error) {
	spreadsheet := excelize.NewFile()
	sheetName := "Products"
	spreadsheet.SetSheetName("Sheet1", sheetName)

	headers := []string{
		"name", "sku", "barcode", "price", "costPrice",
		"quantity", "minStock", "wholesalePrice", "wholesaleMin",
		"category", "unit", "piecesPerUnit",
	}

	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		spreadsheet.SetCellValue(sheetName, cell, header)
	}

	sampleRow := []interface{}{
		"Sample Product", "SKU001", "1234567890", 100, 70,
		50, 10, 85, 20, "Drinks", "pcs", 1,
	}
	for index, value := range sampleRow {
		cell, _ := excelize.CoordinatesToCellName(index+1, 2)
		spreadsheet.SetCellValue(sheetName, cell, value)
	}

	buffer, err := spreadsheet.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func parseFloat(value string) float64 {
	var result float64
	fmt.Sscanf(value, "%f", &result)
	return result
}

func parseInt(value string) int {
	var result int
	fmt.Sscanf(value, "%d", &result)
	return result
}
