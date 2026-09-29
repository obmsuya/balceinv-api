package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/catalog/seeds"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/spreadsheet"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) ListForCompany(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]CatalogProductView, error) {
	catalogProducts, listError := service.repository.ListForCompany(ctx, querier, companyId)
	if listError != nil {
		return nil, listError
	}
	return toViews(catalogProducts), nil
}

func (service *Service) Summary(ctx context.Context, querier database.Querier, companyId uuid.UUID) (SummaryView, error) {
	companyBusinessType, businessTypeError := service.repository.FindCompanyBusinessType(ctx, querier, companyId)
	if businessTypeError != nil {
		return SummaryView{}, businessTypeError
	}

	counts, countError := service.repository.CountPerBusinessType(ctx, querier)
	if countError != nil {
		return SummaryView{}, countError
	}

	summary := SummaryView{
		CompanyBusinessType: companyBusinessType,
		Counts:              counts,
	}
	return summary, nil
}

func (service *Service) Items(ctx context.Context, querier database.Querier, businessType string) ([]CatalogProductView, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return nil, validationError
	}

	catalogProducts, listError := service.repository.ListByBusinessType(ctx, querier, businessType)
	if listError != nil {
		return nil, listError
	}
	return toViews(catalogProducts), nil
}

func (service *Service) Import(ctx context.Context, querier database.Querier, businessType string, importMode string, fileName string, fileReader io.Reader) (ImportResult, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return ImportResult{}, validationError
	}
	isKnownMode := importMode == ImportModeMerge || importMode == ImportModeReplace
	if !isKnownMode {
		return ImportResult{}, ErrImportModeInvalid
	}

	fileRows, readError := spreadsheet.ReadRows(fileName, fileReader)
	if readError != nil {
		return ImportResult{}, readError
	}

	parsedCatalog, parseError := ParseRows(businessType, fileRows, time.Now().UTC())
	if parseError != nil {
		return ImportResult{}, parseError
	}

	importResult := ImportResult{
		BusinessType:  businessType,
		Mode:          importMode,
		RowsRead:      parsedCatalog.RowsRead,
		Skipped:       len(parsedCatalog.Problems),
		Problems:      parsedCatalog.Problems[:min(len(parsedCatalog.Problems), maximumProblems)],
		ProblemsTotal: len(parsedCatalog.Problems),
	}
	hasUsableRows := len(parsedCatalog.Products) > 0
	if !hasUsableRows {
		return importResult, ErrNoValidRows
	}

	existingNameKeys := map[string]bool{}
	if importMode == ImportModeReplace {
		_, clearError := service.repository.DeleteByBusinessType(ctx, querier, businessType)
		if clearError != nil {
			return ImportResult{}, clearError
		}
	}
	if importMode == ImportModeMerge {
		foundNameKeys, listError := service.repository.ListNameKeys(ctx, querier, businessType)
		if listError != nil {
			return ImportResult{}, listError
		}
		existingNameKeys = foundNameKeys
	}

	for _, catalogProduct := range parsedCatalog.Products {
		if existingNameKeys[catalogProduct.NameKey] {
			importResult.Updated++
			continue
		}
		importResult.Added++
	}

	upsertError := service.repository.UpsertAll(ctx, querier, parsedCatalog.Products)
	if upsertError != nil {
		return ImportResult{}, upsertError
	}

	totalInList, countError := service.repository.CountByBusinessType(ctx, querier, businessType)
	if countError != nil {
		return ImportResult{}, countError
	}
	importResult.TotalInList = totalInList

	return importResult, nil
}

func (service *Service) Clear(ctx context.Context, querier database.Querier, businessType string) (ClearResult, error) {
	validationError := ValidateBusinessType(businessType)
	if validationError != nil {
		return ClearResult{}, validationError
	}

	removedCount, clearError := service.repository.DeleteByBusinessType(ctx, querier, businessType)
	if clearError != nil {
		return ClearResult{}, clearError
	}
	return ClearResult{Removed: removedCount}, nil
}

func (service *Service) SeedEmptyLists(ctx context.Context, querier database.Querier) (int, error) {
	return service.seedEmptyListsFrom(ctx, querier, seeds.Files)
}

func (service *Service) seedEmptyListsFrom(ctx context.Context, querier database.Querier, seedFiles fs.FS) (int, error) {
	seedFileNames, globError := fs.Glob(seedFiles, "*.json")
	if globError != nil {
		return 0, fmt.Errorf("failed to list seed lists: %w", globError)
	}

	seededCount := 0
	for _, seedFileName := range seedFileNames {
		businessType := strings.TrimSuffix(path.Base(seedFileName), ".json")
		isValidBusinessType := ValidateBusinessType(businessType) == nil
		if !isValidBusinessType {
			continue
		}

		existingCount, countError := service.repository.CountByBusinessType(ctx, querier, businessType)
		if countError != nil {
			return seededCount, countError
		}
		if existingCount > 0 {
			continue
		}

		seedProducts, seedReadError := readSeedList(seedFiles, seedFileName, time.Now().UTC())
		if seedReadError != nil {
			return seededCount, seedReadError
		}

		upsertError := service.repository.UpsertAll(ctx, querier, seedProducts)
		if upsertError != nil {
			return seededCount, upsertError
		}
		seededCount += len(seedProducts)
	}

	return seededCount, nil
}

func (service *Service) Template() ([]byte, error) {
	workbook := excelize.NewFile()
	defer workbook.Close()

	sheetName := "Common products"
	renameError := workbook.SetSheetName("Sheet1", sheetName)
	if renameError != nil {
		return nil, fmt.Errorf("failed to name the template sheet: %w", renameError)
	}

	templateRows := [][]any{
		{"name", "category", "sub_category", "unit", "sku_prefix", "default_price", "strength", "form"},
		{"Paracetamol 500mg", "Pain relief", "Tablets", "strip", "PARA", 1500, "500mg", "Tablet"},
		{"Amoxicillin 250mg", "Antibiotics", "Capsules", "strip", "AMOX", 3000, "250mg", "Capsule"},
		{"ORS Sachet", "Rehydration", "", "sachet", "ORS", 500, "", "Powder"},
	}
	for rowIndex, templateRow := range templateRows {
		firstCell, cellNameError := excelize.CoordinatesToCellName(1, rowIndex+1)
		if cellNameError != nil {
			return nil, fmt.Errorf("failed to place template row: %w", cellNameError)
		}
		writeRowError := workbook.SetSheetRow(sheetName, firstCell, &templateRow)
		if writeRowError != nil {
			return nil, fmt.Errorf("failed to write template row: %w", writeRowError)
		}
	}

	headerStyleId, headerStyleError := workbook.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if headerStyleError != nil {
		return nil, fmt.Errorf("failed to style the template header: %w", headerStyleError)
	}
	styleError := workbook.SetCellStyle(sheetName, "A1", "H1", headerStyleId)
	if styleError != nil {
		return nil, fmt.Errorf("failed to style the template header: %w", styleError)
	}
	nameWidthError := workbook.SetColWidth(sheetName, "A", "A", 28)
	if nameWidthError != nil {
		return nil, fmt.Errorf("failed to size template columns: %w", nameWidthError)
	}
	otherWidthError := workbook.SetColWidth(sheetName, "B", "H", 16)
	if otherWidthError != nil {
		return nil, fmt.Errorf("failed to size template columns: %w", otherWidthError)
	}

	templateBuffer, writeError := workbook.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("failed to build the template: %w", writeError)
	}
	return templateBuffer.Bytes(), nil
}

func IsImportFileProblem(importError error) bool {
	return errors.Is(importError, spreadsheet.ErrUnsupportedFileType) ||
		errors.Is(importError, spreadsheet.ErrUnreadable) ||
		errors.Is(importError, ErrFileEmpty) ||
		errors.Is(importError, ErrNameColumnMissing) ||
		errors.Is(importError, ErrTooManyRows)
}
