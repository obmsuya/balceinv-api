package invoices

import (
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
)

func TestInvoiceDetailsNameTheCustomerAndTheAmountOnCredit(t *testing.T) {
	customerName := "Mama Rehema"
	customerPhone := "0713222333"
	orderNumber := "ORD-000004"
	saleView := sales.SaleView{
		CustomerName:  &customerName,
		CustomerPhone: &customerPhone,
		OrderNumber:   &orderNumber,
		Total:         9000,
		Payments: []sales.PaymentView{
			{Method: "cash", Amount: 3000},
			{Method: "credit", Amount: 6000},
		},
	}

	for language, wanted := range map[string][3]string{
		documents.English: {"Customer", "Order", "Balance owed (pay later)"},
		documents.Swahili: {"Mteja", "Oda", "Deni (lipa baadaye)"},
	} {
		label := func(key string) string { return invoiceLabels[language][key] }

		detailLabels := map[string]any{}
		for _, detail := range saleDetails(saleView, "Main Shop", label) {
			detailLabels[detail.Label] = detail.Value
		}
		if detailLabels[wanted[0]] != "Mama Rehema · 0713222333" || detailLabels[wanted[1]] != "ORD-000004" {
			t.Fatalf("%s details were %v", language, detailLabels)
		}

		creditShown := false
		for _, totalLine := range totalFields(saleView, false, label) {
			if totalLine.Label == wanted[2] && totalLine.Value == int64(6000) && totalLine.Strong {
				creditShown = true
			}
			if totalLine.Value == int64(6000) && totalLine.Label != wanted[2] {
				t.Fatalf("%s showed the credit as %q", language, totalLine.Label)
			}
		}
		if !creditShown {
			t.Fatalf("%s totals did not show the amount on credit", language)
		}
	}
}
