package accounting_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func postInTransaction(t *testing.T, harness *apptest.Harness, companyId uuid.UUID, post func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error)) (*accounting.PostedEntry, error) {
	t.Helper()
	testContext := context.Background()
	writeTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
	if beginError != nil {
		t.Fatalf("begin: %v", beginError)
	}
	defer writeTransaction.Rollback()
	setTenantError := database.SetTenant(testContext, writeTransaction, harness.Database.IsPostgres(), companyId)
	if setTenantError != nil {
		t.Fatalf("set tenant: %v", setTenantError)
	}

	postedEntry, postError := post(testContext, writeTransaction)
	if postError != nil {
		return nil, postError
	}
	commitError := writeTransaction.Commit()
	if commitError != nil && !errors.Is(commitError, sql.ErrTxDone) {
		t.Fatalf("commit: %v", commitError)
	}
	return postedEntry, nil
}

func TestPostingHooksForSuppliersCustomersAndOrders(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Hook Shop", "owner@hooks.test")
		ledger := accounting.NewLedger(accounting.NewRepository())
		shopId := company.ShopId
		supplierId := uuid.Must(uuid.NewV7())
		customerId := uuid.Must(uuid.NewV7())
		now := time.Now()

		purchase := accounting.PurchasePosting{
			CompanyId: company.Id, PurchaseId: uuid.Must(uuid.NewV7()), ShopId: shopId, ReceivedAt: now,
			NetCost: 100000, Vat: 18000, PaidByMethod: map[string]int64{"cash": 50000, "mobile": 20000}, OwedToSupplier: 48000,
			SupplierId: &supplierId, Reference: "PO-0001",
		}
		postedWhileOff, offError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, purchase)
		})
		if offError != nil || postedWhileOff != nil || entryCount(harness, company.Id) != 0 {
			t.Fatalf("a purchase posted with accounting off: %v %v", postedWhileOff, offError)
		}

		turnOn(t, harness, company.OwnerToken, simpleBooks(true))
		startBooks(t, harness, company.OwnerToken, map[string]any{"mode": "today", "cash_in_drawer": 500000})

		postedPurchase, purchaseError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, purchase)
		})
		if purchaseError != nil || postedPurchase == nil {
			t.Fatalf("purchase posting failed: %v", purchaseError)
		}
		assertLines(t, "purchase", sourceLines(t, harness, company.Id, "purchase", purchase.PurchaseId.String()), map[string]int64{
			"inventory": 100000, "vat_input": 18000, "cash": -50000, "mobile_money": -20000, "payable": -48000,
		})
		supplierParty := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE party_type = 'supplier' AND party_id = $1`, supplierId)
		if supplierParty != 1 {
			t.Fatal("the purchase did not remember its supplier")
		}

		repostedPurchase, repostError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, purchase)
		})
		if repostError != nil || repostedPurchase.Id != postedPurchase.Id {
			t.Fatalf("posting the same purchase twice returned %v %v", repostedPurchase, repostError)
		}

		unevenPurchase := purchase
		unevenPurchase.PurchaseId = uuid.Must(uuid.NewV7())
		unevenPurchase.OwedToSupplier = 1
		_, unevenError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, unevenPurchase)
		})
		if !errors.Is(unevenError, accounting.ErrUnbalanced) {
			t.Fatalf("a purchase that does not add up returned %v", unevenError)
		}
		strangeMethod := purchase
		strangeMethod.PurchaseId = uuid.Must(uuid.NewV7())
		strangeMethod.PaidByMethod = map[string]int64{"goats": 70000}
		_, methodError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, strangeMethod)
		})
		if !errors.Is(methodError, accounting.ErrUnknownMethod) {
			t.Fatalf("an unknown payment method returned %v", methodError)
		}

		supplierPayment := accounting.PaymentPosting{CompanyId: company.Id, SourceId: uuid.Must(uuid.NewV7()), PartyId: &supplierId, ShopId: &shopId, PaidAt: now, Method: "bank", Amount: 48000}
		supplierReturn := accounting.SupplierReturnPosting{
			CompanyId: company.Id, ReturnId: uuid.Must(uuid.NewV7()), ShopId: shopId, ReturnedAt: now, NetCost: 10000, Vat: 1800,
			RefundByMethod: map[string]int64{"cash": 5000}, CreditFromSupplier: 6800, SupplierId: &supplierId,
		}
		customerPayment := accounting.PaymentPosting{CompanyId: company.Id, SourceId: uuid.Must(uuid.NewV7()), PartyId: &customerId, ShopId: &shopId, PaidAt: now, Method: "mobile", Amount: 4000}
		orderDeposit := accounting.PaymentPosting{CompanyId: company.Id, SourceId: uuid.Must(uuid.NewV7()), PartyId: &customerId, ShopId: &shopId, PaidAt: now, Method: "cash", Amount: 3000}
		orderRefund := accounting.PaymentPosting{CompanyId: company.Id, SourceId: uuid.Must(uuid.NewV7()), PartyId: &customerId, ShopId: &shopId, PaidAt: now, Method: "cash", Amount: 1000}
		creditSale := accounting.SalePosting{
			CompanyId: company.Id, SaleId: uuid.Must(uuid.NewV7()), ShopId: shopId, SoldAt: now, Reference: "SALE-1",
			Total: 11800, TaxTotal: 1800, PaidByMethod: map[string]int64{"credit": 6800, "deposit": 2000, "cash": 3500}, ChangeGiven: 500,
			CostTotal: 7000, CustomerId: &customerId,
		}

		hooks := []struct {
			label      string
			sourceType string
			sourceId   uuid.UUID
			post       func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error)
			want       map[string]int64
		}{
			{"supplier payment", "supplier_payment", supplierPayment.SourceId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostSupplierPayment(ctx, querier, supplierPayment)
			}, map[string]int64{"payable": 48000, "bank": -48000}},
			{"supplier return", "supplier_return", supplierReturn.ReturnId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostSupplierReturn(ctx, querier, supplierReturn)
			}, map[string]int64{"cash": 5000, "payable": 6800, "inventory": -10000, "vat_input": -1800}},
			{"customer payment", "customer_payment", customerPayment.SourceId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostCustomerPayment(ctx, querier, customerPayment)
			}, map[string]int64{"mobile_money": 4000, "receivable": -4000}},
			{"order deposit", "order_deposit", orderDeposit.SourceId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostOrderDeposit(ctx, querier, orderDeposit)
			}, map[string]int64{"cash": 3000, "customer_deposits": -3000}},
			{"order refund", "order_refund", orderRefund.SourceId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostOrderRefund(ctx, querier, orderRefund)
			}, map[string]int64{"customer_deposits": 1000, "cash": -1000}},
			{"credit sale collecting a deposit", "sale", creditSale.SaleId, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
				return ledger.PostSale(ctx, querier, creditSale)
			}, map[string]int64{"receivable": 6800, "customer_deposits": 2000, "cash": 3000, "sales": -10000, "vat_output": -1800, "cogs": 7000, "inventory": -7000}},
		}
		for _, hook := range hooks {
			postedEntry, postError := postInTransaction(t, harness, company.Id, hook.post)
			if postError != nil || postedEntry == nil {
				t.Fatalf("%s failed: %v", hook.label, postError)
			}
			assertLines(t, hook.label, sourceLines(t, harness, company.Id, hook.sourceType, hook.sourceId.String()), hook.want)
			again, againError := postInTransaction(t, harness, company.Id, hook.post)
			if againError != nil || again.Id != postedEntry.Id {
				t.Fatalf("%s posted twice", hook.label)
			}
		}

		cancel := accounting.ReversalPosting{
			CompanyId: company.Id, OriginalSourceType: "purchase", OriginalSourceId: purchase.PurchaseId,
			SourceType: "purchase_cancel", ReversedAt: now, Reason: "Wrong supplier",
		}
		cancelled, cancelError := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.ReverseSource(ctx, querier, cancel)
		})
		if cancelError != nil || cancelled == nil {
			t.Fatalf("cancelling the purchase failed: %v", cancelError)
		}
		assertLines(t, "purchase cancel", sourceLines(t, harness, company.Id, "purchase_cancel", purchase.PurchaseId.String()), map[string]int64{
			"inventory": -100000, "vat_input": -18000, "cash": 50000, "mobile_money": 20000, "payable": 48000,
		})
		cancelledAgain, _ := postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.ReverseSource(ctx, querier, cancel)
		})
		if cancelledAgain == nil || cancelledAgain.Id != cancelled.Id {
			t.Fatal("cancelling twice posted a second reversal")
		}

		turnOn(t, harness, company.OwnerToken, map[string]any{"vat_registered": false})
		plainPurchase := purchase
		plainPurchase.PurchaseId = uuid.Must(uuid.NewV7())
		postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostPurchase(ctx, querier, plainPurchase)
		})
		assertLines(t, "purchase without VAT registration", sourceLines(t, harness, company.Id, "purchase", plainPurchase.PurchaseId.String()), map[string]int64{
			"inventory": 118000, "cash": -50000, "mobile_money": -20000, "payable": -48000,
		})

		harness.ExecForCompany(company.Id, `UPDATE accounting_settings SET started_on = $1, closed_until = $2`, localDate(30), localDate(1))
		lateDeposit := accounting.PaymentPosting{CompanyId: company.Id, SourceId: uuid.Must(uuid.NewV7()), PaidAt: localDaysAgo(5), Method: "cash", Amount: 700}
		postInTransaction(t, harness, company.Id, func(ctx context.Context, querier database.Querier) (*accounting.PostedEntry, error) {
			return ledger.PostOrderDeposit(ctx, querier, lateDeposit)
		})
		lateDate := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_id = $1 AND entry_date = $2`, lateDeposit.SourceId, localDate(0))
		if lateDate != 1 {
			t.Fatal("an automatic posting inside a closed month was not moved to the first open day")
		}

		assertEveryEntryBalances(t, harness, company.Id)
	})
}
