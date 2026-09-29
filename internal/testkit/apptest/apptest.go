package apptest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/server"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const AllowedOrigin = "http://app.balce.test"

type Harness struct {
	t            *testing.T
	Database     *database.Database
	App          *fiber.App
	QueryCounter *atomic.Int64
}

type Company struct {
	Id            uuid.UUID
	ShopId        uuid.UUID
	OwnerId       uuid.UUID
	OwnerEmail    string
	OwnerPassword string
	OwnerToken    string
}

type Response struct {
	Status  int
	Body    map[string]any
	Headers http.Header
	Raw     []byte
}

func Start(t *testing.T, engineCase testkit.EngineCase) *Harness {
	t.Helper()

	openDatabase := testkit.OpenMigratedAsApp(t, engineCase)
	queryCounter := &atomic.Int64{}
	openDatabase.QueryCounter = queryCounter

	testConfig := &config.Config{
		Engine:         engineCase.Engine,
		AllowedOrigins: []string{AllowedOrigin},
	}

	objectStore, storeError := storage.NewLocalStore(t.TempDir())
	if storeError != nil {
		t.Fatalf("object store: %v", storeError)
	}

	harness := &Harness{
		t:            t,
		Database:     openDatabase,
		App:          server.New(testConfig, openDatabase, objectStore, func() {}),
		QueryCounter: queryCounter,
	}

	return harness
}

func (harness *Harness) CreateCompany(businessName string, ownerEmail string) Company {
	harness.t.Helper()

	ownerPassword := "owner-password-123"
	setupRequest := tenancy.SetupRequest{
		BusinessName:  businessName,
		OwnerName:     businessName + " Owner",
		OwnerEmail:    ownerEmail,
		OwnerPassword: ownerPassword,
	}

	accessRepository := access.NewRepository()
	usersRepository := users.NewRepository()
	tenancyService := tenancy.NewService(tenancy.NewRepository(), access.NewService(accessRepository), usersRepository, settings.NewRepository())

	setupTransaction, beginError := harness.Database.Writer.BeginTx(context.Background(), nil)
	if beginError != nil {
		harness.t.Fatalf("begin setup: %v", beginError)
	}
	defer setupTransaction.Rollback()

	setupResult, setupError := tenancyService.CreateCompany(context.Background(), setupTransaction, harness.Database.IsPostgres(), setupRequest)
	if setupError != nil {
		harness.t.Fatalf("create company %s: %v", businessName, setupError)
	}

	commitError := setupTransaction.Commit()
	if commitError != nil {
		harness.t.Fatalf("commit setup: %v", commitError)
	}

	createdCompany := Company{
		Id:            setupResult.CompanyId,
		ShopId:        setupResult.ShopId,
		OwnerId:       setupResult.UserId,
		OwnerEmail:    ownerEmail,
		OwnerPassword: ownerPassword,
	}
	createdCompany.OwnerToken = harness.MustLogin(ownerEmail, ownerPassword)

	return createdCompany
}

func (harness *Harness) MustLogin(email string, password string) string {
	harness.t.Helper()

	loginResponse := harness.Send(http.MethodPost, "/api/auth/login", "", map[string]any{
		"email":    email,
		"password": password,
	}, map[string]string{"X-Balce-Client": "desktop"})
	if loginResponse.Status != http.StatusOK {
		harness.t.Fatalf("login %s returned %d: %v", email, loginResponse.Status, loginResponse.Body)
	}

	loginData := loginResponse.Data()
	sessionToken, hasToken := loginData["session_token"].(string)
	if !hasToken || sessionToken == "" {
		harness.t.Fatalf("login %s returned no session token", email)
	}

	return sessionToken
}

func (harness *Harness) Call(method string, path string, sessionToken string, requestBody any) Response {
	harness.t.Helper()
	return harness.Send(method, path, sessionToken, requestBody, nil)
}

func (harness *Harness) Send(method string, path string, sessionToken string, requestBody any, extraHeaders map[string]string) Response {
	harness.t.Helper()

	var bodyReader io.Reader
	hasBody := requestBody != nil
	if hasBody {
		encodedBody, encodeError := json.Marshal(requestBody)
		if encodeError != nil {
			harness.t.Fatalf("encode body: %v", encodeError)
		}
		bodyReader = bytes.NewReader(encodedBody)
	}

	testRequest := httptest.NewRequest(method, path, bodyReader)
	testRequest.Header.Set("Content-Type", "application/json")
	if sessionToken != "" {
		testRequest.Header.Set("Authorization", "Bearer "+sessionToken)
	}
	for headerName, headerValue := range extraHeaders {
		testRequest.Header.Set(headerName, headerValue)
	}

	testResponse, requestError := harness.App.Test(testRequest, 10000)
	if requestError != nil {
		harness.t.Fatalf("%s %s: %v", method, path, requestError)
	}
	defer testResponse.Body.Close()

	responseBytes, readError := io.ReadAll(testResponse.Body)
	if readError != nil {
		harness.t.Fatalf("read body: %v", readError)
	}

	decodedBody := map[string]any{}
	json.Unmarshal(responseBytes, &decodedBody)

	return Response{
		Status:  testResponse.StatusCode,
		Body:    decodedBody,
		Headers: testResponse.Header,
		Raw:     responseBytes,
	}
}

func (harness *Harness) Upload(path string, sessionToken string, fieldName string, fileName string, fileBytes []byte) Response {
	harness.t.Helper()

	formBody := &bytes.Buffer{}
	formWriter := multipart.NewWriter(formBody)
	if fieldName != "" {
		filePart, partError := formWriter.CreateFormFile(fieldName, fileName)
		if partError != nil {
			harness.t.Fatalf("create form file: %v", partError)
		}
		filePart.Write(fileBytes)
	}
	formWriter.Close()

	testRequest := httptest.NewRequest(http.MethodPost, path, formBody)
	testRequest.Header.Set("Content-Type", formWriter.FormDataContentType())
	testRequest.Header.Set("Authorization", "Bearer "+sessionToken)

	testResponse, requestError := harness.App.Test(testRequest, 10000)
	if requestError != nil {
		harness.t.Fatalf("upload %s: %v", path, requestError)
	}
	defer testResponse.Body.Close()

	responseBytes, readError := io.ReadAll(testResponse.Body)
	if readError != nil {
		harness.t.Fatalf("read body: %v", readError)
	}

	decodedBody := map[string]any{}
	json.Unmarshal(responseBytes, &decodedBody)

	return Response{
		Status:  testResponse.StatusCode,
		Body:    decodedBody,
		Headers: testResponse.Header,
		Raw:     responseBytes,
	}
}

func (harness *Harness) CountQueries(runRequest func()) int64 {
	harness.QueryCounter.Store(0)
	runRequest()
	return harness.QueryCounter.Load()
}

func (harness *Harness) ExecForCompany(companyId uuid.UUID, statement string, arguments ...any) {
	harness.t.Helper()
	testContext := context.Background()

	writeTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
	if beginError != nil {
		harness.t.Fatalf("begin: %v", beginError)
	}
	defer writeTransaction.Rollback()

	setTenantError := database.SetTenant(testContext, writeTransaction, harness.Database.IsPostgres(), companyId)
	if setTenantError != nil {
		harness.t.Fatalf("set tenant: %v", setTenantError)
	}

	_, execError := writeTransaction.ExecContext(testContext, statement, arguments...)
	if execError != nil {
		harness.t.Fatalf("exec %q: %v", statement, execError)
	}

	commitError := writeTransaction.Commit()
	if commitError != nil {
		harness.t.Fatalf("commit: %v", commitError)
	}
}

func (harness *Harness) QueryIntForCompany(companyId uuid.UUID, query string, arguments ...any) int64 {
	harness.t.Helper()
	testContext := context.Background()

	readTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
	if beginError != nil {
		harness.t.Fatalf("begin: %v", beginError)
	}
	defer readTransaction.Rollback()

	setTenantError := database.SetTenant(testContext, readTransaction, harness.Database.IsPostgres(), companyId)
	if setTenantError != nil {
		harness.t.Fatalf("set tenant: %v", setTenantError)
	}

	foundValue := int64(0)
	scanError := readTransaction.QueryRowContext(testContext, query, arguments...).Scan(&foundValue)
	if scanError != nil {
		harness.t.Fatalf("query %q: %v", query, scanError)
	}

	return foundValue
}

func (response Response) Data() map[string]any {
	responseData, isObject := response.Body["data"].(map[string]any)
	if !isObject {
		return map[string]any{}
	}
	return responseData
}

func (response Response) Items() []any {
	pageItems, isList := response.Data()["items"].([]any)
	if !isList {
		return []any{}
	}
	return pageItems
}

func (response Response) Code() string {
	errorCode, _ := response.Body["code"].(string)
	return errorCode
}
