package features

type FeaturesView struct {
	SuppliersEnabled      bool    `json:"suppliers_enabled"`
	PurchaseOrdersEnabled bool    `json:"purchase_orders_enabled"`
	CustomersEnabled      bool    `json:"customers_enabled"`
	CreditSalesEnabled    bool    `json:"credit_sales_enabled"`
	CustomerOrdersEnabled bool    `json:"customer_orders_enabled"`
	AccountingMode        string  `json:"accounting_mode"`
	VatRegistered         bool    `json:"vat_registered"`
	VatNumber             *string `json:"vat_number"`
}

type UpdateFeaturesRequest struct {
	SuppliersEnabled      *bool   `json:"suppliers_enabled"`
	PurchaseOrdersEnabled *bool   `json:"purchase_orders_enabled"`
	CustomersEnabled      *bool   `json:"customers_enabled"`
	CreditSalesEnabled    *bool   `json:"credit_sales_enabled"`
	CustomerOrdersEnabled *bool   `json:"customer_orders_enabled"`
	AccountingMode        *string `json:"accounting_mode" validate:"omitnil,oneof=off simple full"`
	VatRegistered         *bool   `json:"vat_registered"`
	VatNumber             *string `json:"vat_number" validate:"omitnil,max=40"`
}

func ToView(companyFeatures Features) FeaturesView {
	return FeaturesView{
		SuppliersEnabled:      companyFeatures.SuppliersEnabled,
		PurchaseOrdersEnabled: companyFeatures.PurchaseOrdersEnabled,
		CustomersEnabled:      companyFeatures.CustomersEnabled,
		CreditSalesEnabled:    companyFeatures.CreditSalesEnabled,
		CustomerOrdersEnabled: companyFeatures.CustomerOrdersEnabled,
		AccountingMode:        companyFeatures.AccountingMode,
		VatRegistered:         companyFeatures.VatRegistered,
		VatNumber:             companyFeatures.VatNumber,
	}
}
