package products

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/xuri/excelize/v2"
)

const (
	MaximumImportBytes      = 5 << 20
	maximumImportRows       = 5000
	maximumReportedProblems = 100
)

var (
	ErrImportFileType       = errors.New("use an Excel (.xlsx) or CSV (.csv) file")
	ErrImportUnreadable     = errors.New("this file could not be read; save it again as .xlsx or .csv and retry")
	ErrImportEmpty          = errors.New("this file has no product rows under the header row")
	ErrImportMissingColumns = errors.New("the header row needs name, sku and price columns")
	ErrImportTooManyRows    = fmt.Errorf("this file has more than %d rows; split it into smaller files", maximumImportRows)
	ErrImportRejected       = errors.New("nothing was imported; fix the rows listed and upload the file again")
)

var templateHeaders = []string{
	"name", "sku", "barcode", "price", "costPrice",
	"quantity", "minStock", "wholesalePrice", "wholesaleMin",
	"category", "unit", "piecesPerUnit",
}

var importFieldByHeader = map[string]string{
	"name":             "name",
	"productname":      "name",
	"item":             "name",
	"itemname":         "name",
	"sku":              "sku",
	"code":             "sku",
	"itemcode":         "sku",
	"barcode":          "barcode",
	"price":            "price",
	"sellingprice":     "price",
	"costprice":        "cost_price",
	"cost":             "cost_price",
	"buyingprice":      "cost_price",
	"quantity":         "quantity",
	"qty":              "quantity",
	"stock":            "quantity",
	"openingquantity":  "quantity",
	"minstock":         "min_stock",
	"minimumstock":     "min_stock",
	"reorderlevel":     "min_stock",
	"wholesaleprice":   "wholesale_price",
	"wholesalemin":     "wholesale_min",
	"wholesaleminimum": "wholesale_min",
	"category":         "category",
	"unit":             "unit",
	"uom":              "unit",
	"piecesperunit":    "pieces_per_unit",
}

type importRow struct {
	rowNumber int
	request   CreateProductRequest
}

func (service *Service) ImportProducts(ctx context.Context, querier database.Querier, principal *identity.Principal, fileName string, fileReader io.Reader) (ImportResultView, error) {
	emptyResult := ImportResultView{
		Problems: []ImportProblem{},
	}

	fileRows, readError := readSpreadsheetRows(fileName, fileReader)
	if readError != nil {
		return emptyResult, readError
	}

	currencyDecimals, decimalsError := service.repository.FindCurrencyDecimals(ctx, querier, principal.CompanyId)
	if decimalsError != nil {
		return emptyResult, decimalsError
	}

	parsedRows, rowProblems, parseError := parseImportRows(fileRows, currencyDecimals, principal.ShopId != nil)
	if parseError != nil {
		return emptyResult, parseError
	}

	duplicateProblems, duplicateError := service.findExistingConflicts(ctx, querier, principal, parsedRows)
	if duplicateError != nil {
		return emptyResult, duplicateError
	}
	rowProblems = append(rowProblems, duplicateProblems...)

	importResult := ImportResultView{
		RowsRead:      len(parsedRows) + countRowsWithProblems(rowProblems, parsedRows),
		Problems:      rowProblems,
		ProblemsTotal: len(rowProblems),
	}
	if len(importResult.Problems) > maximumReportedProblems {
		importResult.Problems = importResult.Problems[:maximumReportedProblems]
	}

	hasProblems := len(rowProblems) > 0
	if hasProblems {
		return importResult, ErrImportRejected
	}

	for _, parsedRow := range parsedRows {
		_, createError := service.createProduct(ctx, querier, principal, parsedRow.request)
		if createError != nil {
			importResult.Problems = []ImportProblem{
				{
					Row:     parsedRow.rowNumber,
					Problem: createError.Error(),
				},
			}
			importResult.ProblemsTotal = 1
			return importResult, ErrImportRejected
		}
		importResult.Created++
	}

	return importResult, nil
}

func (service *Service) ImportTemplate() ([]byte, error) {
	spreadsheet := excelize.NewFile()
	defer spreadsheet.Close()

	sheetName := "Products"
	renameError := spreadsheet.SetSheetName("Sheet1", sheetName)
	if renameError != nil {
		return nil, fmt.Errorf("failed to name the sheet: %w", renameError)
	}

	headerRow := make([]any, 0, len(templateHeaders))
	for _, templateHeader := range templateHeaders {
		headerRow = append(headerRow, templateHeader)
	}
	sampleRow := []any{"Sample Product", "SKU001", "1234567890", 1000, 700, 50, 10, 850, 20, "Drinks", "pcs", 1}

	for rowIndex, templateRow := range [][]any{headerRow, sampleRow} {
		firstCell, cellNameError := excelize.CoordinatesToCellName(1, rowIndex+1)
		if cellNameError != nil {
			return nil, fmt.Errorf("failed to place template row: %w", cellNameError)
		}
		rowWriteError := spreadsheet.SetSheetRow(sheetName, firstCell, &templateRow)
		if rowWriteError != nil {
			return nil, fmt.Errorf("failed to write template row: %w", rowWriteError)
		}
	}

	headerStyleId, headerStyleError := spreadsheet.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if headerStyleError == nil {
		spreadsheet.SetCellStyle(sheetName, "A1", "L1", headerStyleId)
	}
	spreadsheet.SetColWidth(sheetName, "A", "A", 28)
	spreadsheet.SetColWidth(sheetName, "B", "L", 14)

	templateBuffer, writeError := spreadsheet.WriteToBuffer()
	if writeError != nil {
		return nil, fmt.Errorf("failed to build the template: %w", writeError)
	}
	return templateBuffer.Bytes(), nil
}

func (service *Service) findExistingConflicts(ctx context.Context, querier database.Querier, principal *identity.Principal, parsedRows []importRow) ([]ImportProblem, error) {
	candidateSkus := make([]string, 0, len(parsedRows))
	candidateCodes := []string{}
	for _, parsedRow := range parsedRows {
		candidateSkus = append(candidateSkus, NormalizeSku(parsedRow.request.Sku))
		for _, barcodeInput := range parsedRow.request.Barcodes {
			candidateCodes = append(candidateCodes, barcodeInput.Code)
		}
	}

	takenSkus, skuError := service.repository.FindTakenSkus(ctx, querier, principal.CompanyId, candidateSkus)
	if skuError != nil {
		return nil, skuError
	}
	takenCodes, codeError := service.repository.FindTakenBarcodes(ctx, querier, principal.CompanyId, candidateCodes)
	if codeError != nil {
		return nil, codeError
	}

	conflictProblems := []ImportProblem{}
	for _, parsedRow := range parsedRows {
		if takenSkus[NormalizeSku(parsedRow.request.Sku)] {
			conflictProblems = append(conflictProblems, ImportProblem{
				Row:     parsedRow.rowNumber,
				Column:  "sku",
				Problem: fmt.Sprintf("SKU %s already belongs to another product", parsedRow.request.Sku),
			})
		}
		for _, barcodeInput := range parsedRow.request.Barcodes {
			if takenCodes[barcodeInput.Code] {
				conflictProblems = append(conflictProblems, ImportProblem{
					Row:     parsedRow.rowNumber,
					Column:  "barcode",
					Problem: fmt.Sprintf("barcode %s already belongs to another product", barcodeInput.Code),
				})
			}
		}
	}

	return conflictProblems, nil
}
