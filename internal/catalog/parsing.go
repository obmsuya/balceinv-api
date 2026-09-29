package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/spreadsheet"
	"github.com/google/uuid"
)

const (
	ImportModeMerge   = "merge"
	ImportModeReplace = "replace"

	MaximumImportBytes = 4 * 1024 * 1024
	maximumRows        = 20000
	maximumProblems    = 50
	defaultUnit        = "pcs"
	defaultSkuPrefix   = "GEN"
)

var (
	ErrBusinessTypeInvalid = errors.New("choose a business type")
	ErrImportModeInvalid   = errors.New("choose add and update, or replace all")
	ErrFileEmpty           = errors.New("this file has no product rows under the header row")
	ErrNameColumnMissing   = errors.New("the first row needs a column called name")
	ErrTooManyRows         = fmt.Errorf("this file has more than %d rows; split it into smaller files", maximumRows)
	ErrNoValidRows         = errors.New("no row in this file could be used; check the problems listed")
)

var businessTypePattern = regexp.MustCompile(`^[a-z][a-z_]{1,31}$`)

var fieldByHeader = map[string]string{
	"name":          "name",
	"product":       "name",
	"product_name":  "name",
	"item":          "name",
	"item_name":     "name",
	"category":      "category",
	"sub_category":  "sub_category",
	"subcategory":   "sub_category",
	"unit":          "unit",
	"uom":           "unit",
	"sku_prefix":    "sku_prefix",
	"sku":           "sku_prefix",
	"code":          "sku_prefix",
	"default_price": "default_price",
	"price":         "default_price",
	"selling_price": "default_price",
}

type ParsedCatalog struct {
	Products []CatalogProduct
	Problems []RowProblem
	RowsRead int
}

func ValidateBusinessType(businessType string) error {
	isValid := businessTypePattern.MatchString(businessType)
	if !isValid {
		return ErrBusinessTypeInvalid
	}
	return nil
}

func NameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func ParsePrice(rawPrice string) (int64, string) {
	cleanedPrice := strings.ToLower(rawPrice)
	for _, currencyWord := range []string{"tsh", "tzs", "ksh", "kes", "ugx", "usd", "/="} {
		cleanedPrice = strings.ReplaceAll(cleanedPrice, currencyWord, "")
	}
	cleanedPrice = strings.NewReplacer(",", "", " ", "", "=", "").Replace(cleanedPrice)

	isEmpty := cleanedPrice == "" || cleanedPrice == "-"
	if isEmpty {
		return 0, ""
	}

	parsedPrice, parseError := strconv.ParseFloat(cleanedPrice, 64)
	isNumber := parseError == nil && !math.IsNaN(parsedPrice) && !math.IsInf(parsedPrice, 0)
	if !isNumber {
		return 0, fmt.Sprintf("the price %q is not a number", rawPrice)
	}
	if parsedPrice < 0 {
		return 0, "the price is below zero"
	}
	if parsedPrice > 1e12 {
		return 0, "the price is too large"
	}

	return int64(math.Round(parsedPrice)), ""
}

func ParseRows(businessType string, fileRows [][]string, importedAt time.Time) (ParsedCatalog, error) {
	headerRowIndex := -1
	for rowIndex, fileRow := range fileRows {
		if !spreadsheet.IsBlankRow(fileRow) {
			headerRowIndex = rowIndex
			break
		}
	}
	if headerRowIndex == -1 {
		return ParsedCatalog{}, ErrFileEmpty
	}

	headerRow := fileRows[headerRowIndex]
	fieldByColumn := make([]string, len(headerRow))
	metadataKeyByColumn := make([]string, len(headerRow))
	usedFields := map[string]bool{}
	for columnIndex, headerCell := range headerRow {
		headerLabel := strings.TrimSpace(strings.TrimPrefix(headerCell, "\ufeff"))
		if headerLabel == "" {
			continue
		}

		knownField, isKnownHeader := fieldByHeader[normalizeHeader(headerLabel)]
		isFirstColumnForField := isKnownHeader && !usedFields[knownField]
		if isFirstColumnForField {
			fieldByColumn[columnIndex] = knownField
			usedFields[knownField] = true
			continue
		}
		metadataKeyByColumn[columnIndex] = headerLabel
	}
	if !usedFields["name"] {
		return ParsedCatalog{}, ErrNameColumnMissing
	}

	dataRows := fileRows[headerRowIndex+1:]
	if len(dataRows) > maximumRows {
		return ParsedCatalog{}, ErrTooManyRows
	}

	parsedCatalog := ParsedCatalog{
		Products: []CatalogProduct{},
		Problems: []RowProblem{},
	}
	firstRowByNameKey := map[string]int{}
	for dataRowIndex, dataRow := range dataRows {
		if spreadsheet.IsBlankRow(dataRow) {
			continue
		}
		parsedCatalog.RowsRead++
		spreadsheetRowNumber := headerRowIndex + dataRowIndex + 2

		catalogProduct, metadata, rowProblem := readRow(dataRow, fieldByColumn, metadataKeyByColumn)

		nameKey := NameKey(catalogProduct.Name)
		isNameMissing := nameKey == ""
		if isNameMissing && rowProblem == "" {
			rowProblem = "the name is empty"
		}
		firstRowNumber, isRepeatedName := firstRowByNameKey[nameKey]
		if isRepeatedName && rowProblem == "" {
			rowProblem = fmt.Sprintf("same name as row %d", firstRowNumber)
		}
		if rowProblem != "" {
			parsedCatalog.Problems = append(parsedCatalog.Problems, RowProblem{
				Row:     spreadsheetRowNumber,
				Name:    catalogProduct.Name,
				Problem: rowProblem,
			})
			continue
		}

		metadataJson, encodeError := json.Marshal(metadata)
		if encodeError != nil {
			return ParsedCatalog{}, fmt.Errorf("failed to encode row %d details: %w", spreadsheetRowNumber, encodeError)
		}

		firstRowByNameKey[nameKey] = spreadsheetRowNumber
		catalogProduct.Id = uuid.Must(uuid.NewV7())
		catalogProduct.BusinessType = businessType
		catalogProduct.NameKey = nameKey
		catalogProduct.Metadata = metadataJson
		catalogProduct.CreatedAt = importedAt
		catalogProduct.UpdatedAt = importedAt
		parsedCatalog.Products = append(parsedCatalog.Products, catalogProduct)
	}

	if parsedCatalog.RowsRead == 0 {
		return ParsedCatalog{}, ErrFileEmpty
	}
	return parsedCatalog, nil
}

func readRow(dataRow []string, fieldByColumn []string, metadataKeyByColumn []string) (CatalogProduct, map[string]string, string) {
	catalogProduct := CatalogProduct{
		Unit:      defaultUnit,
		SkuPrefix: defaultSkuPrefix,
	}
	metadata := map[string]string{}
	rowProblem := ""

	for columnIndex, rawCell := range dataRow {
		if columnIndex >= len(fieldByColumn) {
			break
		}
		cellValue := strings.Join(strings.Fields(rawCell), " ")
		if cellValue == "" {
			continue
		}

		switch fieldByColumn[columnIndex] {
		case "name":
			catalogProduct.Name = cellValue
		case "category":
			catalogProduct.Category = &cellValue
		case "sub_category":
			catalogProduct.SubCategory = &cellValue
		case "unit":
			catalogProduct.Unit = cellValue
		case "sku_prefix":
			catalogProduct.SkuPrefix = strings.ToUpper(cellValue)
		case "default_price":
			defaultPrice, priceProblem := ParsePrice(cellValue)
			if priceProblem != "" {
				rowProblem = priceProblem
			}
			catalogProduct.DefaultPrice = defaultPrice
		default:
			metadataKey := metadataKeyByColumn[columnIndex]
			if metadataKey != "" {
				metadata[metadataKey] = cellValue
			}
		}
	}

	return catalogProduct, metadata, rowProblem
}

func readSeedList(seedFiles fs.FS, seedFileName string, seededAt time.Time) ([]CatalogProduct, error) {
	seedBytes, readError := fs.ReadFile(seedFiles, seedFileName)
	if readError != nil {
		return nil, fmt.Errorf("failed to read seed list %s: %w", seedFileName, readError)
	}
	isEmptyFile := len(bytes.TrimSpace(seedBytes)) == 0
	if isEmptyFile {
		return nil, nil
	}

	seedEntries := []seedEntry{}
	decodeError := json.Unmarshal(seedBytes, &seedEntries)
	if decodeError != nil {
		return nil, fmt.Errorf("seed list %s is not valid JSON: %w", seedFileName, decodeError)
	}

	businessType := strings.TrimSuffix(seedFileName, ".json")
	seenNameKeys := map[string]bool{}
	seedProducts := make([]CatalogProduct, 0, len(seedEntries))
	for _, entry := range seedEntries {
		nameKey := NameKey(entry.Name)
		isUnusable := nameKey == "" || seenNameKeys[nameKey] || entry.DefaultPrice < 0
		if isUnusable {
			continue
		}
		seenNameKeys[nameKey] = true

		metadataJson, encodeError := json.Marshal(entry.Metadata)
		if encodeError != nil || entry.Metadata == nil {
			metadataJson = []byte("{}")
		}

		seedProduct := CatalogProduct{
			Id:           uuid.Must(uuid.NewV7()),
			BusinessType: businessType,
			Name:         strings.Join(strings.Fields(entry.Name), " "),
			NameKey:      nameKey,
			Category:     entry.Category,
			SubCategory:  entry.SubCategory,
			Unit:         valueOrDefault(entry.Unit, defaultUnit),
			SkuPrefix:    valueOrDefault(entry.SkuPrefix, defaultSkuPrefix),
			DefaultPrice: int64(math.Round(entry.DefaultPrice)),
			Metadata:     metadataJson,
			CreatedAt:    seededAt,
			UpdatedAt:    seededAt,
		}
		seedProducts = append(seedProducts, seedProduct)
	}

	return seedProducts, nil
}

func normalizeHeader(headerLabel string) string {
	lowerHeader := strings.ToLower(strings.TrimSpace(headerLabel))
	return strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(lowerHeader)
}

func valueOrDefault(value string, defaultValue string) string {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return defaultValue
	}
	return trimmedValue
}
