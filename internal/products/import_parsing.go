package products

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/spreadsheet"
)

func parseImportRows(fileRows [][]string, currencyDecimals int, hasActiveShop bool) ([]importRow, []ImportProblem, error) {
	headerRowIndex := -1
	for rowIndex, fileRow := range fileRows {
		if !spreadsheet.IsBlankRow(fileRow) {
			headerRowIndex = rowIndex
			break
		}
	}
	if headerRowIndex == -1 {
		return nil, nil, ErrImportEmpty
	}

	fieldByColumn := map[int]string{}
	for columnIndex, headerCell := range fileRows[headerRowIndex] {
		normalizedHeader := normalizeHeader(headerCell)
		knownField, isKnown := importFieldByHeader[normalizedHeader]
		if isKnown {
			fieldByColumn[columnIndex] = knownField
		}
	}
	hasRequiredColumns := hasField(fieldByColumn, "name") && hasField(fieldByColumn, "sku") && hasField(fieldByColumn, "price")
	if !hasRequiredColumns {
		return nil, nil, ErrImportMissingColumns
	}

	dataRows := fileRows[headerRowIndex+1:]
	if len(dataRows) > maximumImportRows {
		return nil, nil, ErrImportTooManyRows
	}

	parsedRows := []importRow{}
	rowProblems := []ImportProblem{}
	firstRowBySku := map[string]int{}
	firstRowByBarcode := map[string]int{}

	for dataRowIndex, dataRow := range dataRows {
		if spreadsheet.IsBlankRow(dataRow) {
			continue
		}
		spreadsheetRowNumber := headerRowIndex + dataRowIndex + 2

		cellValues := map[string]string{}
		for columnIndex, rawCell := range dataRow {
			fieldName, isKnownColumn := fieldByColumn[columnIndex]
			if isKnownColumn {
				cellValues[fieldName] = strings.Join(strings.Fields(rawCell), " ")
			}
		}

		rowRequest, cellProblems := buildImportRequest(cellValues, currencyDecimals, hasActiveShop)
		for _, cellProblem := range cellProblems {
			cellProblem.Row = spreadsheetRowNumber
			rowProblems = append(rowProblems, cellProblem)
		}

		skuKey := NormalizeSku(rowRequest.Sku)
		if firstSkuRow, isRepeated := firstRowBySku[skuKey]; skuKey != "" && isRepeated {
			rowProblems = append(rowProblems, ImportProblem{Row: spreadsheetRowNumber, Column: "sku", Problem: fmt.Sprintf("same SKU as row %d", firstSkuRow)})
		} else if skuKey != "" {
			firstRowBySku[skuKey] = spreadsheetRowNumber
		}
		for _, barcodeInput := range rowRequest.Barcodes {
			if firstBarcodeRow, isRepeated := firstRowByBarcode[barcodeInput.Code]; isRepeated {
				rowProblems = append(rowProblems, ImportProblem{Row: spreadsheetRowNumber, Column: "barcode", Problem: fmt.Sprintf("same barcode as row %d", firstBarcodeRow)})
			} else {
				firstRowByBarcode[barcodeInput.Code] = spreadsheetRowNumber
			}
		}

		if len(cellProblems) == 0 {
			parsedRows = append(parsedRows, importRow{
				rowNumber: spreadsheetRowNumber,
				request:   rowRequest,
			})
		}
	}

	hasNoRows := len(parsedRows) == 0 && len(rowProblems) == 0
	if hasNoRows {
		return nil, nil, ErrImportEmpty
	}

	return parsedRows, rowProblems, nil
}

func buildImportRequest(cellValues map[string]string, currencyDecimals int, hasActiveShop bool) (CreateProductRequest, []ImportProblem) {
	cellProblems := []ImportProblem{}
	addProblem := func(columnName string, problem string) {
		cellProblems = append(cellProblems, ImportProblem{Column: columnName, Problem: problem})
	}

	productName := cellValues["name"]
	if productName == "" {
		addProblem("name", "the name is empty")
	} else if len(productName) > 160 {
		addProblem("name", "the name is longer than 160 characters")
	}

	productSku := cellValues["sku"]
	if productSku == "" {
		addProblem("sku", "the SKU is empty")
	} else if len(productSku) > 64 {
		addProblem("sku", "the SKU is longer than 64 characters")
	}

	sellingPrice, priceProblem := parseAmount(cellValues["price"], currencyDecimals, true)
	if priceProblem != "" {
		addProblem("price", priceProblem)
	}
	costPrice, costProblem := parseAmount(cellValues["cost_price"], currencyDecimals, false)
	if costProblem != "" {
		addProblem("costPrice", costProblem)
	}

	var wholesalePrice *int64
	if cellValues["wholesale_price"] != "" {
		parsedWholesale, wholesaleProblem := parseAmount(cellValues["wholesale_price"], currencyDecimals, false)
		if wholesaleProblem != "" {
			addProblem("wholesalePrice", wholesaleProblem)
		}
		wholesalePrice = &parsedWholesale
	}

	openingQuantity, quantityProblem := parseCount(cellValues["quantity"], 0)
	if quantityProblem != "" {
		addProblem("quantity", quantityProblem)
	}
	if openingQuantity > 0 && !hasActiveShop {
		addProblem("quantity", "choose a shop before importing opening stock")
	}

	minimumStock, minimumProblem := parseCount(cellValues["min_stock"], 5)
	if minimumProblem != "" {
		addProblem("minStock", minimumProblem)
	}
	wholesaleMinimum, wholesaleMinimumProblem := parseCount(cellValues["wholesale_min"], defaultWholesaleMinimum)
	if wholesaleMinimumProblem != "" {
		addProblem("wholesaleMin", wholesaleMinimumProblem)
	}
	piecesPerUnit, piecesProblem := parseCount(cellValues["pieces_per_unit"], 1)
	if piecesProblem != "" || piecesPerUnit < 1 {
		addProblem("piecesPerUnit", "pieces per unit must be a whole number of at least 1")
	}

	var barcodeInputs []BarcodeInput
	if cellValues["barcode"] != "" {
		barcodeInputs = []BarcodeInput{{Code: cellValues["barcode"], PackSize: 1}}
	}

	var productCategory *string
	if cellValues["category"] != "" {
		categoryValue := cellValues["category"]
		productCategory = &categoryValue
	}

	importRequest := CreateProductRequest{
		ProductFields: ProductFields{
			Sku:            productSku,
			Name:           productName,
			Price:          &sellingPrice,
			CostPrice:      costPrice,
			WholesalePrice: wholesalePrice,
			WholesaleMin:   max(wholesaleMinimum, 1),
			Category:       productCategory,
			Unit:           cellValues["unit"],
			PiecesPerUnit:  max(piecesPerUnit, 1),
			Barcodes:       barcodeInputs,
			MinStock:       &minimumStock,
		},
		OpeningQuantity: openingQuantity,
	}

	return importRequest, cellProblems
}

func parseAmount(rawAmount string, currencyDecimals int, isRequired bool) (int64, string) {
	cleanedAmount := strings.ToLower(rawAmount)
	for _, currencyWord := range []string{"tsh", "tzs", "ksh", "kes", "ugx", "usd", "/="} {
		cleanedAmount = strings.ReplaceAll(cleanedAmount, currencyWord, "")
	}
	cleanedAmount = strings.NewReplacer(",", "", " ", "", "=", "").Replace(cleanedAmount)

	isEmpty := cleanedAmount == "" || cleanedAmount == "-"
	if isEmpty && isRequired {
		return 0, "the price is empty"
	}
	if isEmpty {
		return 0, ""
	}

	parsedAmount, parseError := strconv.ParseFloat(cleanedAmount, 64)
	if parseError != nil || math.IsNaN(parsedAmount) || math.IsInf(parsedAmount, 0) {
		return 0, fmt.Sprintf("%q is not a number", rawAmount)
	}
	if parsedAmount < 0 {
		return 0, "the amount is below zero"
	}
	if parsedAmount > 1e12 {
		return 0, "the amount is too large"
	}

	return int64(math.Round(parsedAmount * math.Pow10(currencyDecimals))), ""
}

func parseCount(rawCount string, defaultCount int) (int, string) {
	cleanedCount := strings.ReplaceAll(strings.TrimSpace(rawCount), ",", "")
	if cleanedCount == "" {
		return defaultCount, ""
	}

	parsedCount, parseError := strconv.ParseFloat(cleanedCount, 64)
	isWholeNumber := parseError == nil && parsedCount == math.Trunc(parsedCount)
	if !isWholeNumber {
		return 0, fmt.Sprintf("%q is not a whole number", rawCount)
	}
	if parsedCount < 0 {
		return 0, "the number is below zero"
	}
	if parsedCount > 1e9 {
		return 0, "the number is too large"
	}

	return int(parsedCount), ""
}

func normalizeHeader(headerCell string) string {
	trimmedHeader := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(headerCell, "\ufeff")))
	return strings.NewReplacer(" ", "", "_", "", "-", "", ".", "").Replace(trimmedHeader)
}

func hasField(fieldByColumn map[int]string, fieldName string) bool {
	for _, mappedField := range fieldByColumn {
		if mappedField == fieldName {
			return true
		}
	}
	return false
}

func countRowsWithProblems(rowProblems []ImportProblem, parsedRows []importRow) int {
	parsedRowNumbers := map[int]bool{}
	for _, parsedRow := range parsedRows {
		parsedRowNumbers[parsedRow.rowNumber] = true
	}
	problemRowNumbers := map[int]bool{}
	for _, rowProblem := range rowProblems {
		if !parsedRowNumbers[rowProblem.Row] {
			problemRowNumbers[rowProblem.Row] = true
		}
	}
	return len(problemRowNumbers)
}
