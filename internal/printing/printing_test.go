package printing_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/printing"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestMoneyAndColumnsFitThermalPaper(t *testing.T) {
	cases := map[string]string{
		printing.FormatMoney(1234567, "TZS", 0): "TZS 1,234,567",
		printing.FormatMoney(123456, "KES", 2):  "KES 1,234.56",
		printing.FormatMoney(5, "KES", 2):       "KES 0.05",
		printing.FormatMoney(-3000, "TZS", 0):   "TZS -3,000",
		printing.FormatMoney(0, "TZS", 0):       "TZS 0",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("formatted %q, want %q", got, want)
		}
	}
	squeezed := printing.LeftRightText("A very long product description here", "TZS 1,000,000", 32)
	if len([]rune(squeezed)) != 32 || !strings.HasSuffix(squeezed, "TZS 1,000,000") {
		t.Fatalf("a long line became %q", squeezed)
	}

	note := "Deliver after lunch"
	tin := "123-456-789"
	receiptView := sales.ReceiptView{
		Sale: sales.SaleView{
			ReceiptNumber: "KKO-20260929-0007", CashierName: "Asha", Total: 11800, TaxTotal: 1800, TaxRateBasisPoints: 1800,
			AmountPaid: 20000, ChangeGiven: 8200, CurrencyCode: "TZS", CreatedAt: time.Date(2026, 9, 29, 20, 30, 0, 0, time.UTC), Note: &note,
			Items:    []sales.LineView{{ProductName: "Sugar 1kg with a name much longer than the paper", Quantity: 2, UnitPrice: 5900, LineTotal: 11800}},
			Payments: []sales.PaymentView{{Method: "cash", Amount: 20000}},
			Fiscal:   &sales.FiscalView{Status: "pending"},
		},
		Company:          sales.ReceiptCompanyView{Name: "Duka la Mama", Tin: &tin},
		Shop:             sales.ReceiptShopView{Name: "Kariakoo"},
		ShowTax:          true,
		ReceiptLanguage:  "sw",
		PaperWidthMillis: 58,
	}
	darEsSalaam, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	receiptBytes := printing.BuildReceipt(receiptView, nil, true, time.Now(), darEsSalaam)
	for _, expected := range []string{"KKO-20260929-0007", "JUMLA", "TZS 11,800", "Chenji", "TZS 8,200", "Inajumuisha VAT 18%", "Risiti ya EFD itafuata", "Maelezo: Deliver after lunch", "Tarehe: 29/09/2026 23:30"} {
		if !bytes.Contains(receiptBytes, []byte(expected)) {
			t.Fatalf("the receipt is missing %q", expected)
		}
	}
	if !bytes.HasSuffix(bytes.TrimRight(receiptBytes, "\x00"), []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) && !bytes.Contains(receiptBytes, []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) {
		t.Fatal("the cash drawer pulse is missing")
	}
	for _, receiptLine := range strings.Split(string(receiptBytes), "\n") {
		printableLine := strings.Map(func(character rune) rune {
			if character < 0x20 || character == 0x7F {
				return -1
			}
			return character
		}, receiptLine)
		if len([]rune(printableLine)) > 40 && !strings.Contains(printableLine, "Duka") {
			t.Fatalf("a line is wider than 58 mm paper: %q", printableLine)
		}
	}
}

func TestReceiptsPrintToTheConfiguredPort(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			cloudHarness := apptest.Start(t, engineCase)
			company := cloudHarness.CreateCompany("Cloud Print", "owner@cloudprint.test")
			if cloudHarness.Call(http.MethodGet, "/api/print/status", company.OwnerToken, nil).Status != http.StatusNotFound {
				t.Fatal("printing routes exist in cloud mode")
			}
			return
		}

		harness := apptest.StartDesktop(t, engineCase)
		company := harness.CreateCompany("Print Shop", "owner@print.test")
		cashierToken := harness.CreateStaff(company, "cashier@print.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		deviceDirectory := t.TempDir()
		printing.UseFakeDevices(t, deviceDirectory)
		portPath := "/dev/ttyFAKE0"
		deviceFile := filepath.Join(deviceDirectory, "ttyFAKE0")
		os.WriteFile(deviceFile, nil, 0o600)

		productId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000, "opening_quantity": 5}).Data()["id"].(string)
		saleId := harness.Call(http.MethodPost, "/api/sales", cashierToken, map[string]any{
			"client_ref": "print-sale-1", "items": []map[string]any{{"product_id": productId, "quantity": 2}},
			"payments": []map[string]any{{"method": "cash", "amount": 5000}},
		}).Data()["id"].(string)

		if harness.Call(http.MethodPost, "/api/print/receipt", cashierToken, map[string]any{"sale_id": saleId}).Code() != "printer_off" {
			t.Fatal("printing with the printer off was not refused")
		}
		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"printer_enabled": true})
		if harness.Call(http.MethodPost, "/api/print/receipt", cashierToken, map[string]any{"sale_id": saleId}).Code() != "printer_not_set" {
			t.Fatal("printing without a port was not refused")
		}

		for _, notAPrinter := range []string{filepath.Join(deviceDirectory, "balce.sqlite"), "/etc/passwd", `\\attacker.example\share`, `C:\Users\Public\run.bat`} {
			refused := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"printer_port": notAPrinter})
			if refused.Status != http.StatusBadRequest || refused.Code() != "invalid_setting" {
				t.Fatalf("the printer port %q was accepted: %d %v", notAPrinter, refused.Status, refused.Body)
			}
			testPrint := harness.Call(http.MethodPost, "/api/print/test", company.OwnerToken, map[string]any{"port": notAPrinter})
			if testPrint.Status != http.StatusBadRequest || testPrint.Code() != "invalid_printer_port" {
				t.Fatalf("a test print to %q returned %d %v", notAPrinter, testPrint.Status, testPrint.Body)
			}
		}
		for _, realPort := range []string{"COM3", `\\.\COM12`, "/dev/cu.usbserial-1420", "/dev/usb/lp0", `\\localhost\POS58`} {
			if harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"printer_port": realPort}).Status != http.StatusOK {
				t.Fatalf("the printer port %q was refused", realPort)
			}
		}

		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"printer_port": "/dev/ttyFAKE9"})
		unplugged := harness.Call(http.MethodPost, "/api/print/receipt", cashierToken, map[string]any{"sale_id": saleId})
		if unplugged.Status != http.StatusBadGateway || unplugged.Code() != "printer_unreachable" {
			t.Fatalf("an unplugged printer returned %d %v", unplugged.Status, unplugged.Body)
		}

		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"printer_port": portPath, "printer_paper_width": 58})
		status := harness.Call(http.MethodGet, "/api/print/status", cashierToken, nil).Data()
		if status["enabled"] != true || status["port"] != portPath || status["paper_width"] != float64(58) {
			t.Fatalf("printer status was %v", status)
		}
		printed := harness.Call(http.MethodPost, "/api/print/receipt", cashierToken, map[string]any{"sale_id": saleId})
		portBytes, _ := os.ReadFile(deviceFile)
		if printed.Status != http.StatusOK || !bytes.Contains(portBytes, []byte("Print Shop")) || !bytes.Contains(portBytes, []byte("TZS 3,000")) {
			t.Fatalf("printing returned %d %v, port got %q", printed.Status, printed.Body, portBytes)
		}

		viewerToken := harness.CreateStaff(company, "viewer@print.test", []string{"sales:view"}, []uuid.UUID{company.ShopId})
		if harness.Call(http.MethodPost, "/api/print/receipt", viewerToken, map[string]any{"sale_id": saleId, "open_drawer": true}).Status != http.StatusForbidden {
			t.Fatal("someone who only views sales opened the cash drawer")
		}
		if harness.Call(http.MethodPost, "/api/print/receipt", viewerToken, map[string]any{"sale_id": saleId}).Status != http.StatusOK {
			t.Fatal("someone who views sales could not reprint a receipt")
		}
		if harness.Call(http.MethodPost, "/api/print/receipt", cashierToken, map[string]any{"sale_id": uuid.NewString()}).Status != http.StatusNotFound {
			t.Fatal("printing an unknown sale did not answer 404")
		}
		if harness.Call(http.MethodPost, "/api/print/test", cashierToken, map[string]any{}).Status != http.StatusForbidden {
			t.Fatal("a cashier ran a test print")
		}
		os.WriteFile(deviceFile, nil, 0o600)
		tested := harness.Call(http.MethodPost, "/api/print/test", company.OwnerToken, map[string]any{"port": portPath})
		testBytes, _ := os.ReadFile(deviceFile)
		if tested.Status != http.StatusOK || !bytes.Contains(testBytes, []byte("Test print OK")) || !bytes.Contains(testBytes, []byte("58 mm, 32 columns")) {
			t.Fatalf("the test print returned %d, port got %q", tested.Status, testBytes)
		}
		if harness.Call(http.MethodGet, "/api/print/devices", company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("listing printer ports failed")
		}
	})
}
