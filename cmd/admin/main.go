package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/validation"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
)

func main() {
	hasSubcommand := len(os.Args) > 1 && os.Args[1] == "create-company"
	if !hasSubcommand {
		fmt.Fprintln(os.Stderr, "usage: admin create-company -business-name NAME -owner-name NAME -owner-email EMAIL [-shop-name NAME] [-currency TZS] [-decimals 0]")
		os.Exit(2)
	}

	commandFlags := flag.NewFlagSet("create-company", flag.ExitOnError)
	businessName := commandFlags.String("business-name", "", "company name shown on receipts")
	ownerName := commandFlags.String("owner-name", "", "owner full name")
	ownerEmail := commandFlags.String("owner-email", "", "owner sign-in email")
	shopName := commandFlags.String("shop-name", "", "first shop name (default Main Shop)")
	currencyCode := commandFlags.String("currency", "TZS", "ISO 4217 currency code")
	currencyDecimals := commandFlags.Int("decimals", 0, "currency decimals: 0 or 2")
	commandFlags.Parse(os.Args[2:])

	loadedConfig, configError := config.Load()
	if configError != nil {
		exitWith(configError)
	}

	oneTimePassword, passwordError := newOneTimePassword()
	if passwordError != nil {
		exitWith(passwordError)
	}

	setupRequest := tenancy.SetupRequest{
		BusinessName:     *businessName,
		ShopName:         *shopName,
		CurrencyCode:     *currencyCode,
		CurrencyDecimals: currencyDecimals,
		OwnerName:        *ownerName,
		OwnerEmail:       *ownerEmail,
		OwnerPassword:    oneTimePassword,
		OwnerMustReset:   true,
	}
	fieldErrors := validation.Validate(setupRequest)
	hasFieldErrors := len(fieldErrors) > 0
	if hasFieldErrors {
		for _, fieldError := range fieldErrors {
			fmt.Fprintf(os.Stderr, "%s %s\n", fieldError.Field, fieldError.Message)
		}
		os.Exit(2)
	}

	commandContext, cancelCommand := context.WithTimeout(context.Background(), time.Minute)
	defer cancelCommand()

	_, migrateError := database.MigrateUp(commandContext, loadedConfig.Engine, loadedConfig.MigrationDatabaseUrl, loadedConfig.SqlitePath)
	if migrateError != nil {
		exitWith(migrateError)
	}

	openDatabase, openError := database.Open(commandContext, loadedConfig.Engine, loadedConfig.DatabaseUrl, loadedConfig.SqlitePath)
	if openError != nil {
		exitWith(openError)
	}
	defer openDatabase.Close()

	accessRepository := access.NewRepository()
	tenancyService := tenancy.NewService(tenancy.NewRepository(), access.NewService(accessRepository), users.NewRepository(), settings.NewRepository())

	setupTransaction, beginError := openDatabase.Writer.BeginTx(commandContext, nil)
	if beginError != nil {
		exitWith(beginError)
	}
	defer setupTransaction.Rollback()

	setupResult, setupError := tenancyService.CreateCompany(commandContext, setupTransaction, openDatabase.IsPostgres(), setupRequest)
	if setupError != nil {
		exitWith(setupError)
	}

	commitError := setupTransaction.Commit()
	if commitError != nil {
		exitWith(commitError)
	}

	fmt.Printf("company created\n  company id: %s\n  shop id:    %s\n  owner:      %s\n  one-time password (must be changed at first sign-in): %s\n",
		setupResult.CompanyId, setupResult.ShopId, *ownerEmail, oneTimePassword)
}

func newOneTimePassword() (string, error) {
	randomBytes := make([]byte, 12)
	_, readError := rand.Read(randomBytes)
	if readError != nil {
		return "", fmt.Errorf("failed to generate password: %w", readError)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func exitWith(commandError error) {
	fmt.Fprintln(os.Stderr, commandError)
	os.Exit(1)
}
