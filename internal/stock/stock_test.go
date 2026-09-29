package stock_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/notifications"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestConcurrentSalesNeverOversell(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Stock Shop", "owner@stock.test")
		testContext := context.Background()
		stockService := stock.NewService(stock.NewRepository(), notifications.NewRepository(), accounting.NewLedger(accounting.NewRepository()))

		productId := uuid.Must(uuid.NewV7())
		harness.ExecForCompany(company.Id, `INSERT INTO products (id, company_id, sku, name, price) VALUES ($1, $2, $3, $4, $5)`, productId, company.Id, "SODA", "Soda", 1000)

		recordInTransaction := func(change int, reason string) error {
			writeTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
			if beginError != nil {
				return beginError
			}
			defer writeTransaction.Rollback()

			setTenantError := database.SetTenant(testContext, writeTransaction, harness.Database.IsPostgres(), company.Id)
			if setTenantError != nil {
				return setTenantError
			}

			_, recordError := stockService.RecordMovement(testContext, writeTransaction, stock.MovementRequest{
				CompanyId: company.Id,
				ShopId:    company.ShopId,
				ProductId: productId,
				Change:    change,
				Reason:    reason,
			})
			if recordError != nil {
				return recordError
			}
			return writeTransaction.Commit()
		}

		openingError := recordInTransaction(5, "opening")
		if openingError != nil {
			t.Fatalf("opening stock: %v", openingError)
		}

		saleAttempts := 10
		var waitGroup sync.WaitGroup
		saleResults := make(chan error, saleAttempts)
		for attemptIndex := 0; attemptIndex < saleAttempts; attemptIndex++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				saleResults <- recordInTransaction(-1, "sale")
			}()
		}
		waitGroup.Wait()
		close(saleResults)

		successfulSales := 0
		refusedSales := 0
		for saleResult := range saleResults {
			switch {
			case saleResult == nil:
				successfulSales++
			case errors.Is(saleResult, stock.ErrInsufficientStock):
				refusedSales++
			default:
				t.Fatalf("unexpected sale error: %v", saleResult)
			}
		}
		if successfulSales != 5 || refusedSales != 5 {
			t.Fatalf("%d sales succeeded and %d were refused, want 5 and 5", successfulSales, refusedSales)
		}

		finalQuantity := harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE shop_id = $1 AND product_id = $2`, company.ShopId, productId)
		movementTotal := harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(change), 0) FROM stock_movements WHERE shop_id = $1 AND product_id = $2`, company.ShopId, productId)
		if finalQuantity != 0 || movementTotal != finalQuantity {
			t.Fatalf("stock is %d and movements sum to %d, want 0 and equal", finalQuantity, movementTotal)
		}
	})
}
