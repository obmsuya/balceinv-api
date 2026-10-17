package cleardata

type ClearRequest struct {
	BusinessName string `json:"business_name" validate:"required,max=120"`
	Password     string `json:"password" validate:"required,max=200"`
}

type ClearResultView struct {
	KeptShopId   string `json:"kept_shop_id"`
	Products     int64  `json:"products"`
	Sales        int64  `json:"sales"`
	Customers    int64  `json:"customers"`
	Suppliers    int64  `json:"suppliers"`
	ShopsRemoved int64  `json:"shops_removed"`
	StaffRemoved int64  `json:"staff_removed"`
	FilesRemoved int    `json:"files_removed"`
}

func toView(summary ClearSummary) ClearResultView {
	return ClearResultView{
		KeptShopId:   summary.KeptShopId,
		Products:     summary.Products,
		Sales:        summary.Sales,
		Customers:    summary.Customers,
		Suppliers:    summary.Suppliers,
		ShopsRemoved: summary.ShopsRemoved,
		StaffRemoved: summary.StaffRemoved,
		FilesRemoved: summary.FilesRemoved,
	}
}
