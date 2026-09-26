package services

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/seeds"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	CatalogImportModeMerge   = "merge"
	CatalogImportModeReplace = "replace"
	catalogMaximumRows       = 20000
	catalogMaximumProblems   = 50
	catalogDefaultUnit       = "pcs"
	catalogDefaultSKUPrefix  = "GEN"
)

var (
	ErrCatalogBusinessTypeInvalid = errors.New("choose a business type")
	ErrCatalogImportModeInvalid   = errors.New("choose add and update, or replace all")
	ErrCatalogFileType            = errors.New("use an Excel (.xlsx) or CSV (.csv) file")
	ErrCatalogFileUnreadable      = errors.New("this file could not be read, save it again as .xlsx or .csv and retry")
	ErrCatalogFileEmpty           = errors.New("this file has no product rows under the header row")
	ErrCatalogNameColumnMissing   = errors.New("the first row needs a column called name")
	ErrCatalogTooManyRows         = fmt.Errorf("this file has more than %d rows, split it into smaller files", catalogMaximumRows)
	ErrCatalogNoValidRows         = errors.New("no row in this file could be used, check the problems listed")
)

var businessTypePattern = regexp.MustCompile(`^[a-z][a-z_]{1,31}$`)

var catalogColumnByHeader = map[string]string{
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

type CatalogRowProblem struct {
	Row     int    `json:"row"`
	Name    string `json:"name,omitempty"`
	Problem string `json:"problem"`
}

type CatalogImportResult struct {
	BusinessType  string              `json:"business_type"`
	Mode          string              `json:"mode"`
	RowsRead      int                 `json:"rows_read"`
	Added         int                 `json:"added"`
	Updated       int                 `json:"updated"`
	Skipped       int                 `json:"skipped"`
	TotalInList   int64               `json:"total_in_list"`
	Problems      []CatalogRowProblem `json:"problems"`
	ProblemsTotal int                 `json:"problems_total"`
}

type CatalogSummary struct {
	CompanyBusinessType string                    `json:"company_business_type"`
	Counts              []repository.CatalogCount `json:"counts"`
}

type CatalogService struct {
	database          *gorm.DB
	catalogRepository *repository.CatalogRepository
}

func NewCatalogService(database *gorm.DB, catalogRepository *repository.CatalogRepository) *CatalogService {
	return &CatalogService{database: database, catalogRepository: catalogRepository}
}

func ValidateBusinessType(businessType string) error {
	if !businessTypePattern.MatchString(businessType) {
		return ErrCatalogBusinessTypeInvalid
	}
	return nil
}

func (service *CatalogService) CompanyBusinessType() (string, error) {
	var settings models.Settings
	settingsLoadError := service.database.First(&settings).Error
	if settingsLoadError != nil {
		return "", fmt.Errorf("could not load settings: %w", settingsLoadError)
	}
	var company models.Company
	companyLoadError := service.database.First(&company, settings.CompanyID).Error
	if companyLoadError != nil {
		return "", fmt.Errorf("could not load company: %w", companyLoadError)
	}
	return company.BusinessType, nil
}

func (service *CatalogService) ListForCompany() ([]models.CatalogProduct, error) {
	companyBusinessType, businessTypeError := service.CompanyBusinessType()
	if businessTypeError != nil {
		return nil, businessTypeError
	}
	return service.catalogRepository.FindByBusinessType(companyBusinessType)
}

func (service *CatalogService) ListForBusinessType(businessType string) ([]models.CatalogProduct, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return nil, validationError
	}
	return service.catalogRepository.FindByBusinessType(businessType)
}

func (service *CatalogService) Summary() (*CatalogSummary, error) {
	catalogCounts, countError := service.catalogRepository.CountPerBusinessType()
	if countError != nil {
		return nil, fmt.Errorf("could not count catalog products: %w", countError)
	}
	companyBusinessType, businessTypeError := service.CompanyBusinessType()
	if businessTypeError != nil {
		companyBusinessType = ""
	}
	return &CatalogSummary{CompanyBusinessType: companyBusinessType, Counts: catalogCounts}, nil
}

func (service *CatalogService) Clear(businessType string) (int64, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return 0, validationError
	}
	return service.catalogRepository.DeleteByBusinessType(businessType)
}

func (service *CatalogService) SeedCompanyCatalogIfEmpty() error {
	companyBusinessType, businessTypeError := service.CompanyBusinessType()
	if businessTypeError != nil {
		return nil
	}
	return service.SeedIfEmpty(companyBusinessType)
}

func (service *CatalogService) SeedIfEmpty(businessType string) error {
	if ValidateBusinessType(businessType) != nil {
		return nil
	}
	existingCount, countError := service.catalogRepository.CountByBusinessType(businessType)
	if countError != nil {
		return fmt.Errorf("could not count catalog products: %w", countError)
	}
	if existingCount > 0 {
		return nil
	}
	seedProducts, seedReadError := readSeedCatalog(seeds.Files, businessType)
	if seedReadError != nil {
		return seedReadError
	}
	return service.catalogRepository.CreateAll(seedProducts)
}

func readSeedCatalog(seedFiles fs.FS, businessType string) ([]models.CatalogProduct, error) {
	seedBytes, seedOpenError := fs.ReadFile(seedFiles, businessType+".json")
	if errors.Is(seedOpenError, fs.ErrNotExist) {
		return nil, nil
	}
	if seedOpenError != nil {
		return nil, fmt.Errorf("could not read the %s seed list: %w", businessType, seedOpenError)
	}
	if len(bytes.TrimSpace(seedBytes)) == 0 {
		return nil, nil
	}
	var seedProducts []models.CatalogProduct
	seedParseError := json.Unmarshal(seedBytes, &seedProducts)
	if seedParseError != nil {
		return nil, fmt.Errorf("the %s seed list is not valid JSON: %w", businessType, seedParseError)
	}
	usableSeedProducts := seedProducts[:0]
	seenNames := map[string]bool{}
	for _, seedProduct := range seedProducts {
		nameKey := repository.CatalogNameKey(seedProduct.Name)
		if nameKey == "" || seenNames[nameKey] {
			continue
		}
		seenNames[nameKey] = true
		seedProduct.ID = 0
		seedProduct.BusinessType = businessType
		seedProduct.Name = strings.Join(strings.Fields(seedProduct.Name), " ")
		if seedProduct.Unit == "" {
			seedProduct.Unit = catalogDefaultUnit
		}
		if seedProduct.SKUPrefix == "" {
			seedProduct.SKUPrefix = catalogDefaultSKUPrefix
		}
		usableSeedProducts = append(usableSeedProducts, seedProduct)
	}
	return usableSeedProducts, nil
}

func (service *CatalogService) Import(businessType string, importMode string, fileName string, fileReader io.Reader) (*CatalogImportResult, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return nil, validationError
	}
	if importMode != CatalogImportModeMerge && importMode != CatalogImportModeReplace {
		return nil, ErrCatalogImportModeInvalid
	}

	fileRows, fileReadError := readCatalogRows(fileName, fileReader)
	if fileReadError != nil {
		return nil, fileReadError
	}

	parsedCatalog, parseError := ParseCatalogRows(businessType, fileRows)
	if parseError != nil {
		return nil, parseError
	}

	importResult := &CatalogImportResult{
		BusinessType:  businessType,
		Mode:          importMode,
		RowsRead:      parsedCatalog.RowsRead,
		Skipped:       len(parsedCatalog.Problems),
		ProblemsTotal: len(parsedCatalog.Problems),
		Problems:      parsedCatalog.Problems,
	}
	if len(importResult.Problems) > catalogMaximumProblems {
		importResult.Problems = importResult.Problems[:catalogMaximumProblems]
	}
	if len(parsedCatalog.Products) == 0 {
		return importResult, ErrCatalogNoValidRows
	}

	if importMode == CatalogImportModeReplace {
		replaceError := service.catalogRepository.Replace(businessType, parsedCatalog.Products)
		if replaceError != nil {
			return nil, fmt.Errorf("could not save the list: %w", replaceError)
		}
		importResult.Added = len(parsedCatalog.Products)
	} else {
		mergeResult, mergeError := service.catalogRepository.Merge(businessType, parsedCatalog.Products)
		if mergeError != nil {
			return nil, fmt.Errorf("could not save the list: %w", mergeError)
		}
		importResult.Added = mergeResult.Added
		importResult.Updated = mergeResult.Updated
	}

	totalInList, countError := service.catalogRepository.CountByBusinessType(businessType)
	if countError != nil {
		return nil, fmt.Errorf("could not count catalog products: %w", countError)
	}
	importResult.TotalInList = totalInList
	return importResult, nil
}

func readCatalogRows(fileName string, fileReader io.Reader) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".xlsx":
		spreadsheet, openError := excelize.OpenReader(fileReader)
		if openError != nil {
			return nil, ErrCatalogFileUnreadable
		}
		defer spreadsheet.Close()
		sheetRows, sheetReadError := spreadsheet.GetRows(spreadsheet.GetSheetName(0))
		if sheetReadError != nil {
			return nil, ErrCatalogFileUnreadable
		}
		return sheetRows, nil
	case ".csv":
		csvBytes, csvReadError := io.ReadAll(fileReader)
		if csvReadError != nil {
			return nil, ErrCatalogFileUnreadable
		}
		return parseCatalogCSV(csvBytes)
	default:
		return nil, ErrCatalogFileType
	}
}

func parseCatalogCSV(csvBytes []byte) ([][]string, error) {
	csvBytes = bytes.TrimPrefix(csvBytes, []byte("\xef\xbb\xbf"))
	csvReader := csv.NewReader(bytes.NewReader(csvBytes))
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true
	firstLine, _, _ := bytes.Cut(csvBytes, []byte("\n"))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		csvReader.Comma = ';'
	}
	csvRows, csvParseError := csvReader.ReadAll()
	if csvParseError != nil {
		return nil, ErrCatalogFileUnreadable
	}
	return csvRows, nil
}

type ParsedCatalog struct {
	Products []models.CatalogProduct
	Problems []CatalogRowProblem
	RowsRead int
}

func ParseCatalogRows(businessType string, fileRows [][]string) (*ParsedCatalog, error) {
	headerRowIndex := -1
	for rowIndex, fileRow := range fileRows {
		if !rowIsBlank(fileRow) {
			headerRowIndex = rowIndex
			break
		}
	}
	if headerRowIndex == -1 {
		return nil, ErrCatalogFileEmpty
	}

	headerRow := fileRows[headerRowIndex]
	fieldByColumn := make([]string, len(headerRow))
	metadataKeyByColumn := make([]string, len(headerRow))
	hasNameColumn := false
	for columnIndex, headerCell := range headerRow {
		headerLabel := strings.TrimSpace(strings.TrimPrefix(headerCell, "\ufeff"))
		if headerLabel == "" {
			continue
		}
		knownField, isKnownColumn := catalogColumnByHeader[normalizeHeader(headerLabel)]
		if isKnownColumn && !columnAlreadyUsed(fieldByColumn, knownField) {
			fieldByColumn[columnIndex] = knownField
			hasNameColumn = hasNameColumn || knownField == "name"
			continue
		}
		metadataKeyByColumn[columnIndex] = headerLabel
	}
	if !hasNameColumn {
		return nil, ErrCatalogNameColumnMissing
	}

	dataRows := fileRows[headerRowIndex+1:]
	if len(dataRows) > catalogMaximumRows {
		return nil, ErrCatalogTooManyRows
	}

	parsedCatalog := &ParsedCatalog{Problems: []CatalogRowProblem{}}
	firstRowByName := map[string]int{}
	for dataRowIndex, dataRow := range dataRows {
		if rowIsBlank(dataRow) {
			continue
		}
		parsedCatalog.RowsRead++
		spreadsheetRowNumber := headerRowIndex + dataRowIndex + 2

		catalogProduct := models.CatalogProduct{
			BusinessType: businessType,
			Unit:         catalogDefaultUnit,
			SKUPrefix:    catalogDefaultSKUPrefix,
		}
		metadata := models.JSONMap{}
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
				catalogProduct.SKUPrefix = strings.ToUpper(cellValue)
			case "default_price":
				defaultPrice, priceProblem := ParseCatalogPrice(cellValue)
				if priceProblem != "" {
					rowProblem = priceProblem
				}
				catalogProduct.DefaultPrice = defaultPrice
			default:
				if metadataKeyByColumn[columnIndex] != "" {
					metadata[metadataKeyByColumn[columnIndex]] = cellValue
				}
			}
		}

		if catalogProduct.Name == "" && rowProblem == "" {
			rowProblem = "the name is empty"
		}
		nameKey := repository.CatalogNameKey(catalogProduct.Name)
		if firstRowNumber, isRepeated := firstRowByName[nameKey]; rowProblem == "" && isRepeated {
			rowProblem = fmt.Sprintf("same name as row %d", firstRowNumber)
		}
		if rowProblem != "" {
			parsedCatalog.Problems = append(parsedCatalog.Problems, CatalogRowProblem{
				Row: spreadsheetRowNumber, Name: catalogProduct.Name, Problem: rowProblem,
			})
			continue
		}

		firstRowByName[nameKey] = spreadsheetRowNumber
		if len(metadata) > 0 {
			catalogProduct.Metadata = metadata
		}
		parsedCatalog.Products = append(parsedCatalog.Products, catalogProduct)
	}

	if parsedCatalog.RowsRead == 0 {
		return nil, ErrCatalogFileEmpty
	}
	return parsedCatalog, nil
}

func ParseCatalogPrice(rawPrice string) (float64, string) {
	cleanedPrice := strings.ToLower(rawPrice)
	for _, currencyWord := range []string{"tsh", "tzs", "/="} {
		cleanedPrice = strings.ReplaceAll(cleanedPrice, currencyWord, "")
	}
	cleanedPrice = strings.NewReplacer(",", "", " ", "", "=", "").Replace(cleanedPrice)
	if cleanedPrice == "" || cleanedPrice == "-" {
		return 0, ""
	}
	parsedPrice, parseError := strconv.ParseFloat(cleanedPrice, 64)
	if parseError != nil {
		return 0, fmt.Sprintf("the price %q is not a number", rawPrice)
	}
	if parsedPrice < 0 {
		return 0, "the price is below zero"
	}
	return parsedPrice, ""
}

func (service *CatalogService) Template() ([]byte, error) {
	spreadsheet := excelize.NewFile()
	defer spreadsheet.Close()
	sheetName := "Common products"
	renameError := spreadsheet.SetSheetName("Sheet1", sheetName)
	if renameError != nil {
		return nil, fmt.Errorf("could not name the sheet: %w", renameError)
	}

	templateRows := [][]interface{}{
		{"name", "category", "sub_category", "unit", "sku_prefix", "default_price", "strength", "form"},
		{"Paracetamol 500mg", "Pain relief", "Tablets", "strip", "PARA", 1500, "500mg", "Tablet"},
		{"Amoxicillin 250mg", "Antibiotics", "Capsules", "strip", "AMOX", 3000, "250mg", "Capsule"},
		{"ORS Sachet", "Rehydration", "", "sachet", "ORS", 500, "", "Powder"},
	}
	for rowIndex, templateRow := range templateRows {
		firstCell, cellNameError := excelize.CoordinatesToCellName(1, rowIndex+1)
		if cellNameError != nil {
			return nil, fmt.Errorf("could not place template row: %w", cellNameError)
		}
		rowWriteError := spreadsheet.SetSheetRow(sheetName, firstCell, &templateRow)
		if rowWriteError != nil {
			return nil, fmt.Errorf("could not write template row: %w", rowWriteError)
		}
	}

	headerStyleId, headerStyleError := spreadsheet.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if headerStyleError == nil {
		spreadsheet.SetCellStyle(sheetName, "A1", "H1", headerStyleId)
	}
	spreadsheet.SetColWidth(sheetName, "A", "A", 28)
	spreadsheet.SetColWidth(sheetName, "B", "H", 16)

	templateBuffer, writeError := spreadsheet.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("could not build the template: %w", writeError)
	}
	return templateBuffer.Bytes(), nil
}

func normalizeHeader(headerLabel string) string {
	lowerHeader := strings.ToLower(strings.TrimSpace(headerLabel))
	return strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(lowerHeader)
}

func columnAlreadyUsed(fieldByColumn []string, field string) bool {
	for _, usedField := range fieldByColumn {
		if usedField == field {
			return true
		}
	}
	return false
}

func rowIsBlank(fileRow []string) bool {
	for _, cellValue := range fileRow {
		if strings.TrimSpace(cellValue) != "" {
			return false
		}
	}
	return true
}
