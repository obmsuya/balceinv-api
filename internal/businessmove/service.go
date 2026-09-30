package businessmove

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	packageFormat      = 1
	manifestEntryName  = "manifest.json"
	databaseEntryName  = "balce.db"
	mediaEntryPrefix   = "media/"
	databaseSizeLimit  = 2 << 30
	mediaFileSizeLimit = 20 << 20
	packageEntryLimit  = 50000
	manifestSizeLimit  = 1 << 16
)

var (
	ErrOwnerOnly        = errors.New("only the owner can move the business")
	ErrInvalidPackage   = errors.New("this is not a Balce moving file")
	ErrNewerPackage     = errors.New("this moving file comes from a newer Balce; update the desktop app or wait for the online update")
	ErrAlreadyMoved     = errors.New("this business is already online")
	ErrEmailAlreadyUsed = errors.New("one of this business's users already has an online account with the same email")
	ErrNotCloud         = errors.New("moving a business needs the online server")
)

var mediaKeyQueries = []string{
	`SELECT logo_key FROM companies WHERE logo_key IS NOT NULL AND logo_key <> ''`,
	`SELECT image_key FROM products WHERE image_key IS NOT NULL AND image_key <> ''`,
	`SELECT attachment_key FROM journal_entries WHERE attachment_key IS NOT NULL AND attachment_key <> ''`,
}

type Service struct {
	openDatabase *database.Database
	objectStore  storage.Store
}

func NewService(openDatabase *database.Database, objectStore storage.Store) *Service {
	return &Service{openDatabase: openDatabase, objectStore: objectStore}
}

func openSqliteReadOnly(databaseFilePath string) (*sql.DB, error) {
	sourceDatabase, openError := sql.Open("sqlite", "file:"+databaseFilePath+"?mode=ro")
	if openError != nil {
		return nil, fmt.Errorf("failed to open business database: %w", openError)
	}
	return sourceDatabase, nil
}

func schemaVersion(ctx context.Context, databaseFilePath string) (int64, error) {
	source, openError := openSqliteReadOnly(databaseFilePath)
	if openError != nil {
		return 0, openError
	}
	defer source.Close()

	var version int64
	scanError := source.QueryRowContext(ctx, `SELECT version FROM schema_migrations`).Scan(&version)
	if scanError != nil {
		return 0, fmt.Errorf("not a Balce database: %w", scanError)
	}
	return version, nil
}

func onlyCompany(ctx context.Context, source *sql.DB) (uuid.UUID, string, error) {
	companyRows, queryError := source.QueryContext(ctx, `SELECT id, name FROM companies`)
	if queryError != nil {
		return uuid.Nil, "", fmt.Errorf("%w: %v", ErrInvalidPackage, queryError)
	}
	defer companyRows.Close()

	var companyIdText, businessName string
	companyCount := 0
	for companyRows.Next() {
		scanError := companyRows.Scan(&companyIdText, &businessName)
		if scanError != nil {
			return uuid.Nil, "", fmt.Errorf("%w: %v", ErrInvalidPackage, scanError)
		}
		companyCount++
	}
	if companyCount != 1 {
		return uuid.Nil, "", fmt.Errorf("%w: it holds %d businesses", ErrInvalidPackage, companyCount)
	}
	companyId, parseError := uuid.Parse(companyIdText)
	if parseError != nil {
		return uuid.Nil, "", fmt.Errorf("%w: %v", ErrInvalidPackage, parseError)
	}
	return companyId, businessName, nil
}

func mediaKeys(ctx context.Context, source *sql.DB) ([]string, error) {
	keys := []string{}
	for _, keyQuery := range mediaKeyQueries {
		keyRows, queryError := source.QueryContext(ctx, keyQuery)
		if queryError != nil {
			return nil, fmt.Errorf("failed to list media: %w", queryError)
		}
		for keyRows.Next() {
			var mediaKey string
			scanError := keyRows.Scan(&mediaKey)
			if scanError != nil {
				keyRows.Close()
				return nil, fmt.Errorf("failed to list media: %w", scanError)
			}
			keys = append(keys, mediaKey)
		}
		keyRows.Close()
	}
	return keys, nil
}

func (service *Service) BuildPackage(ctx context.Context) ([]byte, Manifest, error) {
	workDirectory, makeDirectoryError := os.MkdirTemp("", "balce-move-")
	if makeDirectoryError != nil {
		return nil, Manifest{}, fmt.Errorf("failed to make a work folder: %w", makeDirectoryError)
	}
	defer os.RemoveAll(workDirectory)

	snapshotPath := filepath.Join(workDirectory, databaseEntryName)
	_, vacuumError := service.openDatabase.Writer.ExecContext(ctx, "VACUUM INTO $1", snapshotPath)
	if vacuumError != nil {
		return nil, Manifest{}, fmt.Errorf("failed to copy the business database: %w", vacuumError)
	}

	snapshot, openError := openSqliteReadOnly(snapshotPath)
	if openError != nil {
		return nil, Manifest{}, openError
	}
	defer snapshot.Close()

	companyId, businessName, companyError := onlyCompany(ctx, snapshot)
	if companyError != nil {
		return nil, Manifest{}, companyError
	}
	keys, keysError := mediaKeys(ctx, snapshot)
	if keysError != nil {
		return nil, Manifest{}, keysError
	}

	manifest := Manifest{Format: packageFormat, CompanyId: companyId, BusinessName: businessName, CreatedAt: time.Now().UTC()}
	packageBuffer := &bytes.Buffer{}
	zipWriter := zip.NewWriter(packageBuffer)

	manifestBytes, marshalError := json.Marshal(manifest)
	if marshalError != nil {
		return nil, Manifest{}, fmt.Errorf("failed to write the manifest: %w", marshalError)
	}
	writeError := writeZipEntry(zipWriter, manifestEntryName, bytes.NewReader(manifestBytes))
	if writeError != nil {
		return nil, Manifest{}, writeError
	}

	snapshotFile, snapshotOpenError := os.Open(snapshotPath)
	if snapshotOpenError != nil {
		return nil, Manifest{}, fmt.Errorf("failed to read the business database: %w", snapshotOpenError)
	}
	defer snapshotFile.Close()
	writeError = writeZipEntry(zipWriter, databaseEntryName, snapshotFile)
	if writeError != nil {
		return nil, Manifest{}, writeError
	}

	for _, mediaKey := range keys {
		mediaObject, getError := service.objectStore.Get(ctx, mediaKey)
		if errors.Is(getError, storage.ErrObjectNotFound) || errors.Is(getError, storage.ErrInvalidKey) {
			continue
		}
		if getError != nil {
			return nil, Manifest{}, fmt.Errorf("failed to read picture %s: %w", mediaKey, getError)
		}
		writeError = writeZipEntry(zipWriter, mediaEntryPrefix+mediaKey, bytes.NewReader(mediaObject.Body))
		if writeError != nil {
			return nil, Manifest{}, writeError
		}
	}

	closeError := zipWriter.Close()
	if closeError != nil {
		return nil, Manifest{}, fmt.Errorf("failed to finish the moving file: %w", closeError)
	}
	return packageBuffer.Bytes(), manifest, nil
}

func writeZipEntry(zipWriter *zip.Writer, entryName string, content io.Reader) error {
	entryWriter, createError := zipWriter.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Deflate, Modified: time.Now().UTC()})
	if createError != nil {
		return fmt.Errorf("failed to add %s: %w", entryName, createError)
	}
	_, copyError := io.Copy(entryWriter, content)
	if copyError != nil {
		return fmt.Errorf("failed to add %s: %w", entryName, copyError)
	}
	return nil
}

func readZipEntry(zipFile *zip.File, sizeLimit int64) ([]byte, error) {
	if zipFile.UncompressedSize64 > uint64(sizeLimit) {
		return nil, fmt.Errorf("%w: %s is too large", ErrInvalidPackage, zipFile.Name)
	}
	entryReader, openError := zipFile.Open()
	if openError != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPackage, openError)
	}
	defer entryReader.Close()
	entryBytes, readError := io.ReadAll(io.LimitReader(entryReader, sizeLimit+1))
	if readError != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPackage, readError)
	}
	if int64(len(entryBytes)) > sizeLimit {
		return nil, fmt.Errorf("%w: %s is too large", ErrInvalidPackage, zipFile.Name)
	}
	return entryBytes, nil
}

func extractDatabase(zipFile *zip.File, destinationPath string) error {
	if zipFile.UncompressedSize64 > databaseSizeLimit {
		return fmt.Errorf("%w: the business database is too large", ErrInvalidPackage)
	}
	entryReader, openError := zipFile.Open()
	if openError != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPackage, openError)
	}
	defer entryReader.Close()

	destinationFile, createError := os.Create(destinationPath)
	if createError != nil {
		return fmt.Errorf("failed to unpack the business database: %w", createError)
	}
	defer destinationFile.Close()

	copiedBytes, copyError := io.Copy(destinationFile, io.LimitReader(entryReader, databaseSizeLimit+1))
	if copyError != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPackage, copyError)
	}
	if copiedBytes > databaseSizeLimit {
		return fmt.Errorf("%w: the business database is too large", ErrInvalidPackage)
	}
	return nil
}

func isOwnMediaKey(mediaKey string, companyId uuid.UUID) bool {
	if storage.ValidateKey(mediaKey) != nil {
		return false
	}
	keyParts := strings.SplitN(mediaKey, "/", 3)
	return len(keyParts) == 3 && keyParts[1] == companyId.String()
}

func uniqueViolationOn(queryError error) string {
	var postgresError *pgconn.PgError
	if errors.As(queryError, &postgresError) && postgresError.Code == "23505" {
		return postgresError.ConstraintName
	}
	return ""
}

func (service *Service) ImportPackage(ctx context.Context, packageBytes []byte) (MoveResultView, error) {
	if !service.openDatabase.IsPostgres() {
		return MoveResultView{}, ErrNotCloud
	}

	zipReader, zipError := zip.NewReader(bytes.NewReader(packageBytes), int64(len(packageBytes)))
	if zipError != nil {
		return MoveResultView{}, fmt.Errorf("%w: %v", ErrInvalidPackage, zipError)
	}
	if len(zipReader.File) > packageEntryLimit {
		return MoveResultView{}, fmt.Errorf("%w: too many files", ErrInvalidPackage)
	}

	var manifestFile, databaseFile *zip.File
	mediaFiles := []*zip.File{}
	for _, zipFile := range zipReader.File {
		switch {
		case zipFile.Name == manifestEntryName:
			manifestFile = zipFile
		case zipFile.Name == databaseEntryName:
			databaseFile = zipFile
		case strings.HasPrefix(zipFile.Name, mediaEntryPrefix):
			mediaFiles = append(mediaFiles, zipFile)
		}
	}
	if manifestFile == nil || databaseFile == nil {
		return MoveResultView{}, fmt.Errorf("%w: the manifest or database is missing", ErrInvalidPackage)
	}

	manifestBytes, manifestReadError := readZipEntry(manifestFile, manifestSizeLimit)
	if manifestReadError != nil {
		return MoveResultView{}, manifestReadError
	}
	manifest := Manifest{}
	unmarshalError := json.Unmarshal(manifestBytes, &manifest)
	if unmarshalError != nil || manifest.Format != packageFormat {
		return MoveResultView{}, fmt.Errorf("%w: unknown manifest", ErrInvalidPackage)
	}

	workDirectory, makeDirectoryError := os.MkdirTemp("", "balce-move-in-")
	if makeDirectoryError != nil {
		return MoveResultView{}, fmt.Errorf("failed to make a work folder: %w", makeDirectoryError)
	}
	defer os.RemoveAll(workDirectory)

	databasePath := filepath.Join(workDirectory, databaseEntryName)
	extractError := extractDatabase(databaseFile, databasePath)
	if extractError != nil {
		return MoveResultView{}, extractError
	}
	packageVersion, packageVersionError := schemaVersion(ctx, databasePath)
	if packageVersionError != nil {
		return MoveResultView{}, fmt.Errorf("%w: %v", ErrInvalidPackage, packageVersionError)
	}
	var serverVersion int64
	serverVersionError := service.openDatabase.Reader.QueryRowContext(ctx, `SELECT version FROM schema_migrations`).Scan(&serverVersion)
	if serverVersionError != nil {
		return MoveResultView{}, fmt.Errorf("failed to read the server schema version: %w", serverVersionError)
	}
	if packageVersion > serverVersion {
		return MoveResultView{}, ErrNewerPackage
	}
	_, migrateError := database.MigrateUp(ctx, config.EngineSqlite, "", databasePath)
	if migrateError != nil {
		return MoveResultView{}, fmt.Errorf("%w: %v", ErrInvalidPackage, migrateError)
	}

	source, openError := openSqliteReadOnly(databasePath)
	if openError != nil {
		return MoveResultView{}, openError
	}
	defer source.Close()

	companyId, businessName, companyError := onlyCompany(ctx, source)
	if companyError != nil {
		return MoveResultView{}, companyError
	}
	if companyId != manifest.CompanyId {
		return MoveResultView{}, fmt.Errorf("%w: the manifest names another business", ErrInvalidPackage)
	}

	ownerEmail := ""
	ownerError := source.QueryRowContext(ctx, `
		SELECT u.email FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.is_owner = 1 ORDER BY u.created_at LIMIT 1
	`).Scan(&ownerEmail)
	if ownerError != nil && !errors.Is(ownerError, sql.ErrNoRows) {
		return MoveResultView{}, fmt.Errorf("%w: %v", ErrInvalidPackage, ownerError)
	}

	targetTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return MoveResultView{}, fmt.Errorf("failed to begin the move: %w", beginError)
	}
	defer targetTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, targetTransaction, true, companyId)
	if setTenantError != nil {
		return MoveResultView{}, setTenantError
	}

	tables, tablesError := loadTargetTables(ctx, targetTransaction)
	if tablesError != nil {
		return MoveResultView{}, tablesError
	}
	for _, table := range tables {
		_, copyError := copyTable(ctx, source, targetTransaction, table, companyId)
		if copyError != nil {
			switch uniqueViolationOn(copyError) {
			case "companies_pkey":
				return MoveResultView{}, ErrAlreadyMoved
			case "users_email_unique":
				return MoveResultView{}, ErrEmailAlreadyUsed
			}
			return MoveResultView{}, copyError
		}
	}

	startedAt := time.Now().UTC()
	trialError := tenancy.NewRepository().InsertTrialSubscription(ctx, targetTransaction, companyId, startedAt.AddDate(0, 0, license.TrialDurationDays), startedAt)
	if trialError != nil {
		return MoveResultView{}, trialError
	}

	movedMediaCount := 0
	for _, mediaFile := range mediaFiles {
		mediaKey := strings.TrimPrefix(mediaFile.Name, mediaEntryPrefix)
		if !isOwnMediaKey(mediaKey, companyId) {
			continue
		}
		mediaBytes, mediaReadError := readZipEntry(mediaFile, mediaFileSizeLimit)
		if mediaReadError != nil {
			return MoveResultView{}, mediaReadError
		}
		contentType := mime.TypeByExtension(path.Ext(mediaKey))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		putError := service.objectStore.Put(ctx, mediaKey, contentType, mediaBytes)
		if putError != nil {
			return MoveResultView{}, fmt.Errorf("failed to store picture %s: %w", mediaKey, putError)
		}
		movedMediaCount++
	}

	commitError := targetTransaction.Commit()
	if commitError != nil {
		return MoveResultView{}, fmt.Errorf("failed to finish the move: %w", commitError)
	}
	return MoveResultView{BusinessName: businessName, OwnerEmail: ownerEmail, MediaCount: movedMediaCount}, nil
}
