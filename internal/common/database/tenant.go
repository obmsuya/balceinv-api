package database

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

func SetTenant(ctx context.Context, querier Querier, isPostgres bool, companyId uuid.UUID) error {
	if !isPostgres {
		return nil
	}

	_, setTenantError := querier.ExecContext(ctx, `SELECT set_config('app.company_id', $1, true)`, companyId.String())
	if setTenantError != nil {
		return fmt.Errorf("failed to set tenant: %w", setTenantError)
	}

	return nil
}

func SetAuthLookup(ctx context.Context, querier Querier, isPostgres bool, isEnabled bool) error {
	if !isPostgres {
		return nil
	}

	flagValue := "off"
	if isEnabled {
		flagValue = "on"
	}

	_, setFlagError := querier.ExecContext(ctx, `SELECT set_config('app.auth_lookup', $1, true)`, flagValue)
	if setFlagError != nil {
		return fmt.Errorf("failed to toggle auth lookup: %w", setFlagError)
	}

	return nil
}

func Placeholders(firstIndex int, count int) string {
	placeholderList := make([]string, 0, count)
	for offset := 0; offset < count; offset++ {
		placeholderList = append(placeholderList, "$"+strconv.Itoa(firstIndex+offset))
	}
	return strings.Join(placeholderList, ", ")
}

func IsUniqueViolation(queryError error) bool {
	var postgresError *pgconn.PgError
	if errors.As(queryError, &postgresError) {
		return postgresError.Code == "23505"
	}

	var sqliteError *sqlite.Error
	if errors.As(queryError, &sqliteError) {
		sqliteCode := sqliteError.Code()
		return sqliteCode == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || sqliteCode == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY
	}

	return false
}

func IsForeignKeyViolation(queryError error) bool {
	var postgresError *pgconn.PgError
	if errors.As(queryError, &postgresError) {
		return postgresError.Code == "23503"
	}

	var sqliteError *sqlite.Error
	if errors.As(queryError, &sqliteError) {
		return sqliteError.Code() == sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY
	}

	return false
}

func ToArguments[Value any](values []Value) []any {
	arguments := make([]any, 0, len(values))
	for _, value := range values {
		arguments = append(arguments, value)
	}
	return arguments
}
