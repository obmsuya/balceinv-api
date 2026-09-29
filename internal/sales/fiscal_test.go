package sales_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

type fakeEfd struct {
	server      *httptest.Server
	isDown      atomic.Bool
	mutex       sync.Mutex
	receivedBy  map[string]int
	lastPayload sales.FiscalPayload
	lastAuth    string
	lastIdemKey string
}

func startFakeEfd(t *testing.T) *fakeEfd {
	fake := &fakeEfd{receivedBy: map[string]int{}}
	fake.server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if fake.isDown.Load() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(writer, "maintenance")
			return
		}
		payload := sales.FiscalPayload{}
		json.NewDecoder(request.Body).Decode(&payload)
		fake.mutex.Lock()
		fake.receivedBy[payload.SaleId.String()]++
		fake.lastPayload = payload
		fake.lastAuth = request.Header.Get("Authorization")
		fake.lastIdemKey = request.Header.Get("Idempotency-Key")
		fake.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"verification_code":"V-%s","verification_url":"https://verify.example/%s","extra":1}`, payload.ReceiptNumber, payload.ReceiptNumber)
	}))
	t.Cleanup(fake.server.Close)
	sales.UseFiscalTransport(fake.server.Client().Transport)
	return fake
}

func (fake *fakeEfd) timesReceived(saleId string) int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.receivedBy[saleId]
}

func TestEfdReceiptsAreQueuedSentOnceAndRetried(t *testing.T) {
	fake := startFakeEfd(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Efd Till", "owner@efdtill.test")
		otherCompany := harness.CreateCompany("Other Efd", "owner@otherefd.test")
		secretKey := "efd-test-key-9d1e"
		juiceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "JUICE", "name": "Juice", "price": 2360, "opening_quantity": 50})

		beforeEfd := sell(harness, company.OwnerToken, "efd-before-1", []map[string]any{line(juiceId, 1)}, cash(2360))
		if beforeEfd.Status != http.StatusCreated || beforeEfd.Data()["fiscal"] != nil {
			t.Fatalf("a sale with EFD off returned %d fiscal %v", beforeEfd.Status, beforeEfd.Data()["fiscal"])
		}
		beforeEfdId := beforeEfd.Data()["id"].(string)
		if harness.Call(http.MethodPost, "/api/sales/"+beforeEfdId+"/fiscal", company.OwnerToken, nil).Code() != "efd_unavailable" {
			t.Fatal("sending with EFD off was not refused")
		}

		turnOn := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{
			"efd_enabled": true, "efd_endpoint": fake.server.URL + "/receipts", "efd_api_key": secretKey,
		})
		if turnOn.Status != http.StatusOK {
			t.Fatalf("turning EFD on returned %d %v", turnOn.Status, turnOn.Body)
		}
		if harness.Call(http.MethodPost, "/api/sales/"+beforeEfdId+"/fiscal", company.OwnerToken, nil).Status != http.StatusConflict {
			t.Fatal("a sale made while EFD was off was sent")
		}

		fake.isDown.Store(true)
		whileDown := sell(harness, company.OwnerToken, "efd-down-1", []map[string]any{line(juiceId, 2)}, cash(5000))
		downFiscal, _ := whileDown.Data()["fiscal"].(map[string]any)
		if whileDown.Status != http.StatusCreated || downFiscal["status"] != "pending" {
			t.Fatalf("a sale with EFD on returned %d fiscal %v", whileDown.Status, whileDown.Data()["fiscal"])
		}
		downSaleId := whileDown.Data()["id"].(string)

		refused := harness.Call(http.MethodPost, "/api/sales/"+downSaleId+"/fiscal", company.OwnerToken, nil)
		refusedError, _ := refused.Data()["last_error"].(string)
		if refused.Status != http.StatusOK || refused.Data()["status"] != "failed" || refused.Data()["attempts"] != float64(1) || !strings.Contains(refusedError, "503") {
			t.Fatalf("a refused receipt returned %d %v", refused.Status, refused.Data())
		}
		if strings.Contains(string(refused.Raw), secretKey) {
			t.Fatal("the EFD key leaked into a response")
		}
		waiting := harness.Call(http.MethodGet, "/api/sales?fiscal=waiting", company.OwnerToken, nil)
		if waiting.Data()["total"] != float64(1) || waiting.Items()[0].(map[string]any)["fiscal_status"] != "failed" {
			t.Fatalf("the waiting list was %v", waiting.Data())
		}
		if harness.Call(http.MethodGet, "/api/sales?fiscal=maybe", company.OwnerToken, nil).Status != http.StatusBadRequest {
			t.Fatal("an unknown fiscal filter was accepted")
		}

		fake.isDown.Store(false)
		sentWaiting := harness.Call(http.MethodPost, "/api/sales/fiscal/send-waiting", company.OwnerToken, nil)
		if sentWaiting.Status != http.StatusOK || sentWaiting.Data()["sent"] != float64(1) || sentWaiting.Data()["still_waiting"] != float64(0) {
			t.Fatalf("sending the waiting receipts returned %d %v", sentWaiting.Status, sentWaiting.Data())
		}
		sentSale := harness.Call(http.MethodGet, "/api/sales/"+downSaleId, company.OwnerToken, nil).Data()
		sentFiscal := sentSale["fiscal"].(map[string]any)
		receiptNumber := sentSale["receipt_number"].(string)
		if sentFiscal["status"] != "sent" || sentFiscal["verification_code"] != "V-"+receiptNumber || sentFiscal["verification_url"] != "https://verify.example/"+receiptNumber || sentFiscal["last_error"] != nil {
			t.Fatalf("the sent receipt shows %v", sentFiscal)
		}

		fake.mutex.Lock()
		lastPayload, lastAuth, lastIdemKey := fake.lastPayload, fake.lastAuth, fake.lastIdemKey
		fake.mutex.Unlock()
		if lastAuth != "Bearer "+secretKey || lastIdemKey != downSaleId {
			t.Fatalf("the EFD got auth %q and idempotency key %q", lastAuth, lastIdemKey)
		}
		if lastPayload.ReceiptNumber != receiptNumber || lastPayload.Totals.Total != 4720 || lastPayload.Totals.Tax != 720 || lastPayload.Totals.TotalExcludingTax != 4000 || len(lastPayload.Items) != 1 || lastPayload.Items[0].Quantity != 2 || lastPayload.Currency.Code == "" || len(lastPayload.LocalDate) != 10 {
			t.Fatalf("the EFD payload was %+v", lastPayload)
		}

		resent := harness.Call(http.MethodPost, "/api/sales/"+downSaleId+"/fiscal", company.OwnerToken, nil)
		if resent.Data()["status"] != "sent" || fake.timesReceived(downSaleId) != 1 {
			t.Fatalf("sending a sent receipt again reached the EFD %d times", fake.timesReceived(downSaleId))
		}

		racedSale := sell(harness, company.OwnerToken, "efd-race-1", []map[string]any{line(juiceId, 1)}, cash(2360))
		racedSaleId := racedSale.Data()["id"].(string)
		var raceGroup sync.WaitGroup
		for attempt := 0; attempt < 5; attempt++ {
			raceGroup.Add(1)
			go func() {
				defer raceGroup.Done()
				harness.Call(http.MethodPost, "/api/sales/"+racedSaleId+"/fiscal", company.OwnerToken, nil)
			}()
		}
		raceGroup.Wait()
		if fake.timesReceived(racedSaleId) != 1 {
			t.Fatalf("five tills sending at once reached the EFD %d times", fake.timesReceived(racedSaleId))
		}

		closedServer := httptest.NewTLSServer(http.NotFoundHandler())
		closedUrl := closedServer.URL
		closedServer.Close()
		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"efd_endpoint": closedUrl})
		offlineSale := sell(harness, company.OwnerToken, "efd-offline-1", []map[string]any{line(juiceId, 1)}, cash(2360))
		if offlineSale.Status != http.StatusCreated {
			t.Fatalf("selling with the EFD unreachable returned %d", offlineSale.Status)
		}
		unreachable := harness.Call(http.MethodPost, "/api/sales/"+offlineSale.Data()["id"].(string)+"/fiscal", company.OwnerToken, nil)
		unreachableError, _ := unreachable.Data()["last_error"].(string)
		if unreachable.Data()["status"] != "failed" || !strings.Contains(unreachableError, "Could not reach") {
			t.Fatalf("an unreachable EFD returned %v", unreachable.Data())
		}

		if harness.Call(http.MethodPost, "/api/sales/"+downSaleId+"/fiscal", otherCompany.OwnerToken, nil).Status == http.StatusOK {
			t.Fatal("another company sent this company's receipt")
		}
	})
}
