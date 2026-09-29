package spreadsheet

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

var (
	ErrUnsupportedFileType = errors.New("use an Excel (.xlsx) or CSV (.csv) file")
	ErrUnreadable          = errors.New("this file could not be read; save it again as .xlsx or .csv and retry")
)

func ReadRows(fileName string, fileReader io.Reader) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".xlsx":
		workbook, openError := excelize.OpenReader(fileReader)
		if openError != nil {
			return nil, ErrUnreadable
		}
		defer workbook.Close()

		sheetRows, sheetReadError := workbook.GetRows(workbook.GetSheetName(0))
		if sheetReadError != nil {
			return nil, ErrUnreadable
		}
		return sheetRows, nil
	case ".csv":
		csvBytes, csvReadError := io.ReadAll(fileReader)
		if csvReadError != nil {
			return nil, ErrUnreadable
		}
		return ParseCsv(csvBytes)
	default:
		return nil, ErrUnsupportedFileType
	}
}

func ParseCsv(csvBytes []byte) ([][]string, error) {
	withoutByteOrderMark := bytes.TrimPrefix(csvBytes, []byte("\xef\xbb\xbf"))
	csvReader := csv.NewReader(bytes.NewReader(withoutByteOrderMark))
	csvReader.FieldsPerRecord = -1
	csvReader.LazyQuotes = true

	firstLine, _, _ := bytes.Cut(withoutByteOrderMark, []byte("\n"))
	usesSemicolons := bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(","))
	if usesSemicolons {
		csvReader.Comma = ';'
	}

	csvRows, parseError := csvReader.ReadAll()
	if parseError != nil {
		return nil, ErrUnreadable
	}
	return csvRows, nil
}

func IsBlankRow(fileRow []string) bool {
	for _, cellValue := range fileRow {
		if strings.TrimSpace(cellValue) != "" {
			return false
		}
	}
	return true
}
