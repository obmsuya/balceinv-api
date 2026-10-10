package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/admin"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

func runCreateStaff() {
	commandFlags := flag.NewFlagSet("create-staff", flag.ExitOnError)
	email := commandFlags.String("email", "", "team member sign-in email")
	name := commandFlags.String("name", "", "team member full name")
	role := commandFlags.String("role", admin.RoleSupport, "support or admin")
	commandFlags.Parse(os.Args[2:])

	isRoleKnown := *role == admin.RoleSupport || *role == admin.RoleAdmin
	hasDetails := strings.Contains(*email, "@") && strings.TrimSpace(*name) != ""
	if !isRoleKnown || !hasDetails {
		fmt.Fprintln(os.Stderr, usageText)
		os.Exit(2)
	}

	loadedConfig, configError := config.Load()
	if configError != nil {
		exitWith(configError)
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

	staffTransaction, beginError := openDatabase.Writer.BeginTx(commandContext, nil)
	if beginError != nil {
		exitWith(beginError)
	}
	defer staffTransaction.Rollback()

	adminService := admin.NewService(admin.NewRepository(), nil, openDatabase.IsPostgres())
	oneTimePassword, totpSecret, saveError := adminService.SaveStaff(commandContext, staffTransaction, *email, *name, *role)
	if saveError != nil {
		exitWith(saveError)
	}

	commitError := staffTransaction.Commit()
	if commitError != nil {
		exitWith(commitError)
	}

	fmt.Printf("team member saved (running this again for the same email replaces the password and code)\n  email:    %s\n  role:     %s\n  password: %s\n  authenticator key: %s\n  authenticator link: %s\n",
		strings.ToLower(strings.TrimSpace(*email)), *role, oneTimePassword, totpSecret, admin.TotpSetupLink(totpSecret, *email))
}
