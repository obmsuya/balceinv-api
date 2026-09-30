package businessmove

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const insertBatchRows = 200

var tablesNotMoved = map[string]bool{
	"sessions":              true,
	"company_subscriptions": true,
	"support_messages":      true,
}

type targetTable struct {
	name                string
	columnTypes         map[string]string
	columnOrder         []string
	selfReferenceColumn string
}

func quoted(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func loadTargetTables(ctx context.Context, target database.Querier) ([]targetTable, error) {
	columnRows, columnsError := target.QueryContext(ctx, `
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name, c.ordinal_position
	`)
	if columnsError != nil {
		return nil, fmt.Errorf("failed to read target columns: %w", columnsError)
	}
	defer columnRows.Close()

	tablesByName := map[string]*targetTable{}
	for columnRows.Next() {
		var tableName, columnName, dataType string
		scanError := columnRows.Scan(&tableName, &columnName, &dataType)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan target column: %w", scanError)
		}
		table, isKnown := tablesByName[tableName]
		if !isKnown {
			table = &targetTable{name: tableName, columnTypes: map[string]string{}}
			tablesByName[tableName] = table
		}
		table.columnTypes[columnName] = dataType
		table.columnOrder = append(table.columnOrder, columnName)
	}
	rowsError := columnRows.Err()
	if rowsError != nil {
		return nil, fmt.Errorf("failed to read target columns: %w", rowsError)
	}

	movedTables := map[string]*targetTable{}
	for tableName, table := range tablesByName {
		_, hasCompanyColumn := table.columnTypes["company_id"]
		isMoved := (hasCompanyColumn || tableName == "companies") && !tablesNotMoved[tableName]
		if isMoved {
			movedTables[tableName] = table
		}
	}

	edgeRows, edgesError := target.QueryContext(ctx, `
		SELECT c.conrelid::regclass::text, c.confrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey)
		WHERE c.contype = 'f' AND c.connamespace = 'public'::regnamespace
	`)
	if edgesError != nil {
		return nil, fmt.Errorf("failed to read target foreign keys: %w", edgesError)
	}
	defer edgeRows.Close()

	parentsOf := map[string]map[string]bool{}
	for edgeRows.Next() {
		var childTable, parentTable, childColumn string
		scanError := edgeRows.Scan(&childTable, &parentTable, &childColumn)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan target foreign key: %w", scanError)
		}
		child, isMovedChild := movedTables[childTable]
		if !isMovedChild {
			continue
		}
		if childTable == parentTable {
			if childColumn != "company_id" {
				child.selfReferenceColumn = childColumn
			}
			continue
		}
		if movedTables[parentTable] == nil {
			continue
		}
		if parentsOf[childTable] == nil {
			parentsOf[childTable] = map[string]bool{}
		}
		parentsOf[childTable][parentTable] = true
	}
	edgesRowsError := edgeRows.Err()
	if edgesRowsError != nil {
		return nil, fmt.Errorf("failed to read target foreign keys: %w", edgesRowsError)
	}

	return orderParentsFirst(movedTables, parentsOf)
}

func orderParentsFirst(movedTables map[string]*targetTable, parentsOf map[string]map[string]bool) ([]targetTable, error) {
	remainingNames := make([]string, 0, len(movedTables))
	for tableName := range movedTables {
		remainingNames = append(remainingNames, tableName)
	}
	sort.Strings(remainingNames)

	placed := map[string]bool{}
	ordered := make([]targetTable, 0, len(movedTables))
	for len(remainingNames) > 0 {
		stillWaiting := []string{}
		for _, tableName := range remainingNames {
			allParentsPlaced := true
			for parentName := range parentsOf[tableName] {
				if !placed[parentName] {
					allParentsPlaced = false
				}
			}
			if allParentsPlaced {
				placed[tableName] = true
				ordered = append(ordered, *movedTables[tableName])
				continue
			}
			stillWaiting = append(stillWaiting, tableName)
		}
		if len(stillWaiting) == len(remainingNames) {
			return nil, fmt.Errorf("tables reference each other in a loop: %s", strings.Join(stillWaiting, ", "))
		}
		remainingNames = stillWaiting
	}
	return ordered, nil
}

func sourceColumnNames(ctx context.Context, source *sql.DB, tableName string) (map[string]bool, error) {
	pragmaRows, pragmaError := source.QueryContext(ctx, `SELECT name FROM pragma_table_info($1)`, tableName)
	if pragmaError != nil {
		return nil, fmt.Errorf("failed to read source columns of %s: %w", tableName, pragmaError)
	}
	defer pragmaRows.Close()

	columnNames := map[string]bool{}
	for pragmaRows.Next() {
		var columnName string
		scanError := pragmaRows.Scan(&columnName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan source column of %s: %w", tableName, scanError)
		}
		columnNames[columnName] = true
	}
	return columnNames, pragmaRows.Err()
}

func copyTable(ctx context.Context, source *sql.DB, target database.Querier, table targetTable, companyId uuid.UUID) (int, error) {
	presentInSource, sourceColumnsError := sourceColumnNames(ctx, source, table.name)
	if sourceColumnsError != nil {
		return 0, sourceColumnsError
	}
	if len(presentInSource) == 0 {
		return 0, nil
	}

	sharedColumns := []string{}
	for _, columnName := range table.columnOrder {
		if presentInSource[columnName] {
			sharedColumns = append(sharedColumns, columnName)
		}
	}

	quotedColumns := make([]string, len(sharedColumns))
	for columnIndex, columnName := range sharedColumns {
		quotedColumns[columnIndex] = quoted(columnName)
	}
	ownerColumn := "company_id"
	if table.name == "companies" {
		ownerColumn = "id"
	}
	orderClause := ""
	if table.selfReferenceColumn != "" {
		orderClause = " ORDER BY " + quoted(table.selfReferenceColumn) + " IS NOT NULL"
		if presentInSource["created_at"] {
			orderClause += ", created_at"
		}
	}
	selectQuery := "SELECT " + strings.Join(quotedColumns, ", ") + " FROM " + quoted(table.name) + " WHERE " + quoted(ownerColumn) + " = $1" + orderClause

	sourceRows, selectError := source.QueryContext(ctx, selectQuery, companyId.String())
	if selectError != nil {
		return 0, fmt.Errorf("failed to read %s: %w", table.name, selectError)
	}
	defer sourceRows.Close()

	copiedCount := 0
	pendingValues := make([]any, 0, insertBatchRows*len(sharedColumns))
	pendingRows := 0
	flush := func() error {
		if pendingRows == 0 {
			return nil
		}
		insertError := insertBatch(ctx, target, table.name, quotedColumns, pendingValues, pendingRows)
		if insertError != nil {
			return insertError
		}
		copiedCount += pendingRows
		pendingValues = pendingValues[:0]
		pendingRows = 0
		return nil
	}

	for sourceRows.Next() {
		rowValues := make([]any, len(sharedColumns))
		rowPointers := make([]any, len(sharedColumns))
		for columnIndex := range rowValues {
			rowPointers[columnIndex] = &rowValues[columnIndex]
		}
		scanError := sourceRows.Scan(rowPointers...)
		if scanError != nil {
			return copiedCount, fmt.Errorf("failed to scan %s: %w", table.name, scanError)
		}
		for columnIndex, columnName := range sharedColumns {
			targetValue, convertError := toTargetValue(rowValues[columnIndex], table.columnTypes[columnName])
			if convertError != nil {
				return copiedCount, fmt.Errorf("%s.%s: %w", table.name, columnName, convertError)
			}
			pendingValues = append(pendingValues, targetValue)
		}
		pendingRows++
		if pendingRows == insertBatchRows {
			flushError := flush()
			if flushError != nil {
				return copiedCount, flushError
			}
		}
	}
	rowsError := sourceRows.Err()
	if rowsError != nil {
		return copiedCount, fmt.Errorf("failed to read %s: %w", table.name, rowsError)
	}
	flushError := flush()
	return copiedCount, flushError
}

func insertBatch(ctx context.Context, target database.Querier, tableName string, quotedColumns []string, values []any, rowCount int) error {
	rowPlaceholders := make([]string, rowCount)
	parameterNumber := 1
	for rowIndex := 0; rowIndex < rowCount; rowIndex++ {
		columnPlaceholders := make([]string, len(quotedColumns))
		for columnIndex := range quotedColumns {
			columnPlaceholders[columnIndex] = fmt.Sprintf("$%d", parameterNumber)
			parameterNumber++
		}
		rowPlaceholders[rowIndex] = "(" + strings.Join(columnPlaceholders, ", ") + ")"
	}
	insertQuery := "INSERT INTO " + quoted(tableName) + " (" + strings.Join(quotedColumns, ", ") + ") VALUES " + strings.Join(rowPlaceholders, ", ")
	_, insertError := target.ExecContext(ctx, insertQuery, values...)
	if insertError != nil {
		return fmt.Errorf("failed to insert into %s: %w", tableName, insertError)
	}
	return nil
}

var sourceTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02",
}

func toTargetValue(sourceValue any, targetType string) (any, error) {
	if sourceValue == nil {
		return nil, nil
	}
	if sourceBytes, isBytes := sourceValue.([]byte); isBytes {
		sourceValue = string(sourceBytes)
	}

	switch targetType {
	case "boolean":
		switch typedValue := sourceValue.(type) {
		case bool:
			return typedValue, nil
		case int64:
			return typedValue != 0, nil
		case string:
			return typedValue == "1" || strings.EqualFold(typedValue, "true"), nil
		}
	case "timestamp with time zone", "timestamp without time zone":
		switch typedValue := sourceValue.(type) {
		case time.Time:
			return typedValue.UTC(), nil
		case string:
			for _, layout := range sourceTimeLayouts {
				parsedTime, parseError := time.Parse(layout, typedValue)
				if parseError == nil {
					return parsedTime.UTC(), nil
				}
			}
			return nil, fmt.Errorf("unreadable time %q", typedValue)
		}
	case "bigint", "integer", "smallint":
		switch typedValue := sourceValue.(type) {
		case float64:
			return int64(typedValue), nil
		case bool:
			if typedValue {
				return int64(1), nil
			}
			return int64(0), nil
		}
	case "uuid", "text", "jsonb":
		if timeValue, isTime := sourceValue.(time.Time); isTime {
			return timeValue.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return sourceValue, nil
}
