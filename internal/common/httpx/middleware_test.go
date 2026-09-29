package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
)

func insertCompany(c *fiber.Ctx) error {
	insertQuery := `INSERT INTO companies (id, name) VALUES ($1, $2)`
	_, insertError := httpx.RequestQuerier(c).ExecContext(c.UserContext(), insertQuery, uuid.Must(uuid.NewV7()), c.Path())
	return insertError
}

func buildTestApplication(openDatabase *database.Database) *fiber.App {
	application := fiber.New(fiber.Config{
		ErrorHandler: httpx.ErrorHandler,
	})
	application.Use(httpx.RequestLogging(func() {}))
	application.Use(recover.New())

	apiRoutes := application.Group("/api", httpx.RequestTransaction(openDatabase))

	apiRoutes.Post("/commits", func(c *fiber.Ctx) error {
		insertError := insertCompany(c)
		if insertError != nil {
			return insertError
		}
		return response.Created(c, "created", nil)
	})

	apiRoutes.Post("/conflicts", func(c *fiber.Ctx) error {
		insertError := insertCompany(c)
		if insertError != nil {
			return insertError
		}
		return response.Error(c, fiber.StatusConflict, "conflict", "already exists")
	})

	apiRoutes.Post("/fails", func(c *fiber.Ctx) error {
		insertError := insertCompany(c)
		if insertError != nil {
			return insertError
		}
		return errors.New("downstream exploded")
	})

	apiRoutes.Post("/panics", func(c *fiber.Ctx) error {
		insertError := insertCompany(c)
		if insertError != nil {
			return insertError
		}
		panic("handler bug")
	})

	apiRoutes.Get("/writes-on-get", func(c *fiber.Ctx) error {
		insertError := insertCompany(c)
		if insertError != nil {
			return insertError
		}
		return response.Success(c, "should not happen", nil)
	})

	apiRoutes.Get("/page", func(c *fiber.Ctx) error {
		pagination := httpx.ParsePagination(c)
		return response.Success(c, "page", pagination)
	})

	return application
}

func countCompanies(t *testing.T, openDatabase *database.Database) int {
	t.Helper()
	companyCount := 0
	countError := openDatabase.Reader.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM companies`).Scan(&companyCount)
	if countError != nil {
		t.Fatalf("count companies: %v", countError)
	}
	return companyCount
}

func performRequest(t *testing.T, application *fiber.App, method string, path string) (int, map[string]any) {
	t.Helper()

	testRequest := httptest.NewRequest(method, path, nil)
	testResponse, requestError := application.Test(testRequest, 5000)
	if requestError != nil {
		t.Fatalf("%s %s: %v", method, path, requestError)
	}
	defer testResponse.Body.Close()

	responseBytes, readError := io.ReadAll(testResponse.Body)
	if readError != nil {
		t.Fatalf("read body: %v", readError)
	}

	decodedBody := map[string]any{}
	json.Unmarshal(responseBytes, &decodedBody)

	return testResponse.StatusCode, decodedBody
}

func TestRequestTransactionCommitsOnlySuccessfulWrites(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		application := buildTestApplication(openDatabase)

		failingRequests := []struct {
			path           string
			expectedStatus int
		}{
			{path: "/api/conflicts", expectedStatus: fiber.StatusConflict},
			{path: "/api/fails", expectedStatus: fiber.StatusInternalServerError},
			{path: "/api/panics", expectedStatus: fiber.StatusInternalServerError},
		}

		for _, failingRequest := range failingRequests {
			status, body := performRequest(t, application, fiber.MethodPost, failingRequest.path)
			if status != failingRequest.expectedStatus {
				t.Fatalf("%s returned %d, want %d", failingRequest.path, status, failingRequest.expectedStatus)
			}
			requestId, hasRequestId := body["requestId"].(string)
			if !hasRequestId || requestId == "" {
				t.Fatalf("%s error body has no requestId: %v", failingRequest.path, body)
			}
		}

		if leftoverCount := countCompanies(t, openDatabase); leftoverCount != 0 {
			t.Fatalf("failed requests left %d rows behind", leftoverCount)
		}

		commitStatus, _ := performRequest(t, application, fiber.MethodPost, "/api/commits")
		if commitStatus != fiber.StatusCreated {
			t.Fatalf("commit route returned %d after a panic; the writer connection may have leaked", commitStatus)
		}
		if committedCount := countCompanies(t, openDatabase); committedCount != 1 {
			t.Fatalf("expected 1 committed row, found %d", committedCount)
		}

		writeOnGetStatus, _ := performRequest(t, application, fiber.MethodGet, "/api/writes-on-get")
		if writeOnGetStatus != fiber.StatusInternalServerError {
			t.Fatalf("a write inside GET returned %d, want 500", writeOnGetStatus)
		}
		if finalCount := countCompanies(t, openDatabase); finalCount != 1 {
			t.Fatalf("GET managed to write: %d rows", finalCount)
		}
	})
}

func TestPaginationClampsToAllowedRange(t *testing.T) {
	sqliteCase := testkit.EngineCase{
		Engine:     "sqlite",
		SqlitePath: t.TempDir() + "/pagination.db",
	}
	application := buildTestApplication(testkit.OpenMigrated(t, sqliteCase))

	paginationCases := []struct {
		query          string
		expectedLimit  float64
		expectedOffset float64
	}{
		{query: "", expectedLimit: 20, expectedOffset: 0},
		{query: "?limit=0", expectedLimit: 20, expectedOffset: 0},
		{query: "?limit=-1", expectedLimit: 20, expectedOffset: 0},
		{query: "?limit=abc&offset=xyz", expectedLimit: 20, expectedOffset: 0},
		{query: "?limit=101", expectedLimit: 100, expectedOffset: 0},
		{query: "?limit=50&offset=-5", expectedLimit: 50, expectedOffset: 0},
		{query: "?limit=10&offset=40", expectedLimit: 10, expectedOffset: 40},
	}

	for _, paginationCase := range paginationCases {
		_, body := performRequest(t, application, fiber.MethodGet, "/api/page"+paginationCase.query)
		pageData, _ := body["data"].(map[string]any)

		if pageData["Limit"] != paginationCase.expectedLimit || pageData["Offset"] != paginationCase.expectedOffset {
			t.Fatalf("query %q gave limit=%v offset=%v, want %v/%v",
				paginationCase.query, pageData["Limit"], pageData["Offset"],
				paginationCase.expectedLimit, paginationCase.expectedOffset)
		}
	}
}
