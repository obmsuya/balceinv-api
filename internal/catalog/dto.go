package catalog

import (
	"encoding/json"

	"github.com/google/uuid"
)

type CatalogProductView struct {
	Id           uuid.UUID       `json:"id"`
	BusinessType string          `json:"business_type"`
	Name         string          `json:"name"`
	Category     *string         `json:"category"`
	SubCategory  *string         `json:"sub_category"`
	Unit         string          `json:"unit"`
	SkuPrefix    string          `json:"sku_prefix"`
	DefaultPrice int64           `json:"default_price"`
	Metadata     json.RawMessage `json:"metadata"`
}

type SummaryView struct {
	CompanyBusinessType string              `json:"company_business_type"`
	Counts              []BusinessTypeCount `json:"counts"`
}

type RowProblem struct {
	Row     int    `json:"row"`
	Name    string `json:"name,omitempty"`
	Problem string `json:"problem"`
}

type ImportResult struct {
	BusinessType  string       `json:"business_type"`
	Mode          string       `json:"mode"`
	RowsRead      int          `json:"rows_read"`
	Added         int          `json:"added"`
	Updated       int          `json:"updated"`
	Skipped       int          `json:"skipped"`
	TotalInList   int64        `json:"total_in_list"`
	Problems      []RowProblem `json:"problems"`
	ProblemsTotal int          `json:"problems_total"`
}

type ClearResult struct {
	Removed int64 `json:"removed"`
}

type seedEntry struct {
	Name         string         `json:"name"`
	Category     *string        `json:"category"`
	SubCategory  *string        `json:"sub_category"`
	Unit         string         `json:"unit"`
	SkuPrefix    string         `json:"sku_prefix"`
	DefaultPrice float64        `json:"default_price"`
	Metadata     map[string]any `json:"metadata"`
}

func toView(catalogProduct CatalogProduct) CatalogProductView {
	metadataJson := json.RawMessage(catalogProduct.Metadata)
	hasMetadata := json.Valid(metadataJson)
	if !hasMetadata {
		metadataJson = json.RawMessage("{}")
	}

	return CatalogProductView{
		Id:           catalogProduct.Id,
		BusinessType: catalogProduct.BusinessType,
		Name:         catalogProduct.Name,
		Category:     catalogProduct.Category,
		SubCategory:  catalogProduct.SubCategory,
		Unit:         catalogProduct.Unit,
		SkuPrefix:    catalogProduct.SkuPrefix,
		DefaultPrice: catalogProduct.DefaultPrice,
		Metadata:     metadataJson,
	}
}

func toViews(catalogProducts []CatalogProduct) []CatalogProductView {
	views := make([]CatalogProductView, 0, len(catalogProducts))
	for _, catalogProduct := range catalogProducts {
		views = append(views, toView(catalogProduct))
	}
	return views
}
