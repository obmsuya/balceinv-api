package products

import (
	"encoding/json"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/media"
)

func buildProduct(baseProduct Product, fields ProductFields) (Product, error) {
	metadataJson, metadataError := encodeMetadata(fields.Metadata)
	if metadataError != nil {
		return Product{}, metadataError
	}

	builtProduct := baseProduct
	builtProduct.Sku = NormalizeSku(fields.Sku)
	builtProduct.Name = strings.Join(strings.Fields(fields.Name), " ")
	builtProduct.VariantLabel = strings.TrimSpace(fields.VariantLabel)
	builtProduct.Price = *fields.Price
	builtProduct.CostPrice = fields.CostPrice
	builtProduct.WholesalePrice = fields.WholesalePrice
	builtProduct.WholesaleMin = valueOrDefault(fields.WholesaleMin, defaultWholesaleMinimum)
	builtProduct.Category = optionalText(fields.Category)
	builtProduct.Unit = strings.TrimSpace(fields.Unit)
	if builtProduct.Unit == "" {
		builtProduct.Unit = defaultUnit
	}
	builtProduct.PiecesPerUnit = valueOrDefault(fields.PiecesPerUnit, 1)
	builtProduct.Metadata = metadataJson
	return builtProduct, nil
}

func encodeMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		return []byte("{}"), nil
	}
	if len(metadata) > maximumMetadataKeys {
		return nil, ErrInvalidMetadata
	}

	for metadataKey, metadataValue := range metadata {
		isKeyTooLong := len(metadataKey) == 0 || len(metadataKey) > maximumMetadataKeyLength
		if isKeyTooLong {
			return nil, ErrInvalidMetadata
		}
		switch typedValue := metadataValue.(type) {
		case string:
			if len(typedValue) > maximumMetadataTextLength {
				return nil, ErrInvalidMetadata
			}
		case float64, bool:
		default:
			return nil, ErrInvalidMetadata
		}
	}

	return json.Marshal(metadata)
}

func NormalizeSku(rawSku string) string {
	return strings.ToUpper(strings.TrimSpace(rawSku))
}

func optionalText(rawText *string) *string {
	if rawText == nil {
		return nil
	}
	trimmedText := strings.TrimSpace(*rawText)
	if trimmedText == "" {
		return nil
	}
	return &trimmedText
}

func valueOrDefault(value int, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func toProductView(foundProduct Product, productBarcodes []Barcode) ProductView {
	barcodeViews := make([]BarcodeView, 0, len(productBarcodes))
	for _, productBarcode := range productBarcodes {
		barcodeViews = append(barcodeViews, BarcodeView{
			Code:     productBarcode.Code,
			PackSize: productBarcode.PackSize,
		})
	}

	decodedMetadata := map[string]any{}
	json.Unmarshal(foundProduct.Metadata, &decodedMetadata)

	return ProductView{
		Id:             foundProduct.Id,
		ParentId:       foundProduct.ParentId,
		Sku:            foundProduct.Sku,
		Name:           foundProduct.Name,
		VariantLabel:   foundProduct.VariantLabel,
		Price:          foundProduct.Price,
		CostPrice:      foundProduct.CostPrice,
		WholesalePrice: foundProduct.WholesalePrice,
		WholesaleMin:   foundProduct.WholesaleMin,
		Category:       foundProduct.Category,
		Unit:           foundProduct.Unit,
		PiecesPerUnit:  foundProduct.PiecesPerUnit,
		ImageUrl:       media.PublicUrl(foundProduct.ImageKey),
		Metadata:       decodedMetadata,
		IsActive:       foundProduct.IsActive,
		Quantity:       foundProduct.Quantity,
		MinStock:       foundProduct.MinimumStock,
		VariantCount:   foundProduct.VariantCount,
		Barcodes:       barcodeViews,
		CreatedAt:      foundProduct.CreatedAt,
		UpdatedAt:      foundProduct.UpdatedAt,
	}
}
