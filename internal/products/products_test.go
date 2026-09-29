package products_test

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func productBody(sku string, price int64, extra map[string]any) map[string]any {
	body := map[string]any{
		"sku":   sku,
		"name":  "Product " + sku,
		"price": price,
	}
	for key, value := range extra {
		body[key] = value
	}
	return body
}

func createProduct(t *testing.T, harness *apptest.Harness, sessionToken string, body map[string]any) map[string]any {
	t.Helper()
	createResponse := harness.Call(http.MethodPost, "/api/products", sessionToken, body)
	if createResponse.Status != http.StatusCreated {
		t.Fatalf("create %v returned %d: %v", body["sku"], createResponse.Status, createResponse.Body)
	}
	return createResponse.Data()
}

func TestProductRulesVariantsAndPriceHistory(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Product Shop", "owner@product.test")
		otherCompany := harness.CreateCompany("Rival Products", "owner@rivalproducts.test")

		soda := createProduct(t, harness, company.OwnerToken, productBody("soda-500", 1200, map[string]any{
			"cost_price":       800,
			"category":         "Drinks",
			"opening_quantity": 12,
			"barcodes":         []map[string]any{{"code": "6001001", "pack_size": 1}, {"code": "6001024", "pack_size": 24}},
			"metadata":         map[string]any{"colour": "red", "volume_ml": 500, "chilled": true},
		}))
		if soda["sku"] != "SODA-500" || soda["quantity"] != float64(12) || soda["min_stock"] != float64(5) {
			t.Fatalf("unexpected created product: %v", soda)
		}
		sodaBarcodes, _ := soda["barcodes"].([]any)
		if len(sodaBarcodes) != 2 {
			t.Fatalf("expected 2 barcodes, got %v", soda["barcodes"])
		}
		openingMovements := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE product_id = $1 AND reason = 'opening' AND change = 12`, soda["id"])
		if openingMovements != 1 {
			t.Fatalf("expected one opening movement of 12, found %d", openingMovements)
		}

		createProduct(t, harness, otherCompany.OwnerToken, productBody("SODA-500", 1, nil))
		sameSkuDifferentCase := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, productBody("Soda-500", 1, nil))
		if sameSkuDifferentCase.Status != http.StatusConflict {
			t.Fatalf("duplicate SKU in another case returned %d, want 409", sameSkuDifferentCase.Status)
		}

		productsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products`)
		takenBarcode := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, productBody("WATER", 500, map[string]any{
			"barcodes": []map[string]any{{"code": "6001001"}},
		}))
		if takenBarcode.Status != http.StatusConflict || takenBarcode.Code() != "barcode_taken" {
			t.Fatalf("taken barcode returned %d %s, want 409 barcode_taken", takenBarcode.Status, takenBarcode.Code())
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products`) != productsBefore {
			t.Fatal("a product was left behind after its barcode was rejected")
		}

		rejectedBodies := []map[string]any{
			productBody("NEG", -1, nil),
			productBody("DUPCODE", 100, map[string]any{"barcodes": []map[string]any{{"code": "123"}, {"code": "123"}}}),
			productBody("META", 100, map[string]any{"metadata": map[string]any{"nested": map[string]any{"deep": 1}}}),
			{"sku": "NOPRICE", "name": "No price"},
		}
		for _, rejectedBody := range rejectedBodies {
			rejected := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, rejectedBody)
			if rejected.Status != http.StatusBadRequest {
				t.Fatalf("%v returned %d, want 400", rejectedBody, rejected.Status)
			}
		}
		freeSample := createProduct(t, harness, company.OwnerToken, productBody("FREE", 0, nil))
		if freeSample["price"] != float64(0) {
			t.Fatalf("a zero price must be allowed: %v", freeSample)
		}

		sodaId := soda["id"].(string)
		largeVariant := createProduct(t, harness, company.OwnerToken, productBody("SODA-1L", 2000, map[string]any{"parent_id": sodaId, "variant_label": "1 litre"}))
		missingLabel := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, productBody("SODA-2L", 3000, map[string]any{"parent_id": sodaId}))
		nestedVariant := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, productBody("SODA-1L-X", 3000, map[string]any{"parent_id": largeVariant["id"], "variant_label": "x"}))
		otherCompanyProducts := harness.Call(http.MethodGet, "/api/products", otherCompany.OwnerToken, nil).Items()
		foreignParentId := otherCompanyProducts[0].(map[string]any)["id"].(string)
		foreignParent := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, productBody("STOLEN", 100, map[string]any{"parent_id": foreignParentId, "variant_label": "x"}))
		if missingLabel.Status != http.StatusBadRequest || nestedVariant.Status != http.StatusBadRequest || foreignParent.Status != http.StatusNotFound {
			t.Fatalf("variant rules returned %d/%d/%d, want 400/400/404", missingLabel.Status, nestedVariant.Status, foreignParent.Status)
		}

		variants := harness.Call(http.MethodGet, "/api/products/"+sodaId+"/variants", company.OwnerToken, nil)
		variantList, _ := variants.Body["data"].([]any)
		sodaAgain := harness.Call(http.MethodGet, "/api/products/"+sodaId, company.OwnerToken, nil).Data()
		if len(variantList) != 1 || sodaAgain["variant_count"] != float64(1) {
			t.Fatalf("variants %d, variant_count %v; want 1 and 1", len(variantList), sodaAgain["variant_count"])
		}

		updateBody := productBody("SODA-500", 1500, map[string]any{"name": "Soda 500ml", "category": "Drinks", "min_stock": 8})
		updated := harness.Call(http.MethodPut, "/api/products/"+sodaId, company.OwnerToken, updateBody)
		if updated.Status != http.StatusOK || updated.Data()["price"] != float64(1500) || updated.Data()["min_stock"] != float64(8) {
			t.Fatalf("update returned %d: %v", updated.Status, updated.Data())
		}
		keptBarcodes, _ := updated.Data()["barcodes"].([]any)
		if len(keptBarcodes) != 2 {
			t.Fatal("barcodes must stay when an update does not mention them")
		}
		priceChanges := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM price_history WHERE product_id = $1 AND old_price = 1200 AND new_price = 1500`, sodaId)
		if priceChanges != 1 {
			t.Fatalf("expected one price history row, found %d", priceChanges)
		}
		clearBarcodes := harness.Call(http.MethodPut, "/api/products/"+sodaId, company.OwnerToken, productBody("SODA-500", 1500, map[string]any{"barcodes": []any{}}))
		clearedBarcodes, _ := clearBarcodes.Data()["barcodes"].([]any)
		if len(clearedBarcodes) != 0 {
			t.Fatal("an empty barcode list must clear the barcodes")
		}
		skuClash := harness.Call(http.MethodPut, "/api/products/"+sodaId, company.OwnerToken, productBody("FREE", 1500, nil))
		if skuClash.Status != http.StatusConflict {
			t.Fatalf("renaming to a taken SKU returned %d, want 409", skuClash.Status)
		}

		archive := harness.Call(http.MethodDelete, "/api/products/"+sodaId, company.OwnerToken, nil)
		if archive.Status != http.StatusOK {
			t.Fatalf("archive returned %d", archive.Status)
		}
		activeList := harness.Call(http.MethodGet, "/api/products?limit=100", company.OwnerToken, nil)
		for _, listedItem := range activeList.Items() {
			if listedItem.(map[string]any)["id"] == sodaId {
				t.Fatal("an archived product is still listed")
			}
		}
		withArchived := harness.Call(http.MethodGet, "/api/products?include_archived=true&limit=100", company.OwnerToken, nil)
		if withArchived.Data()["total"] != float64(2) {
			t.Fatalf("include_archived total %v, want 2", withArchived.Data()["total"])
		}
		archivedVariant := harness.Call(http.MethodGet, "/api/products/"+largeVariant["id"].(string), company.OwnerToken, nil)
		if archivedVariant.Data()["is_active"] != false {
			t.Fatal("archiving a product must archive its variants")
		}

		restore := harness.Call(http.MethodPost, "/api/products/"+sodaId+"/restore", company.OwnerToken, nil)
		if restore.Status != http.StatusOK || restore.Data()["is_active"] != true {
			t.Fatalf("restore returned %d %v", restore.Status, restore.Body)
		}
		restoredVariant := harness.Call(http.MethodGet, "/api/products/"+largeVariant["id"].(string), company.OwnerToken, nil)
		if restoredVariant.Data()["is_active"] != true {
			t.Fatal("restoring a product must restore its variants")
		}
	})
}

func TestProductListSearchIsolationAndPermissions(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Big Catalog", "owner@bigcatalog.test")
		otherCompany := harness.CreateCompany("Other Catalog", "owner@othercatalog.test")

		for productNumber := 1; productNumber <= 200; productNumber++ {
			category := "Food"
			if productNumber%2 == 0 {
				category = "Drinks"
			}
			createProduct(t, harness, company.OwnerToken, productBody(fmt.Sprintf("ITEM-%03d", productNumber), int64(productNumber*100), map[string]any{
				"category": category,
				"barcodes": []map[string]any{{"code": fmt.Sprintf("900%04d", productNumber)}},
			}))
		}

		var firstPage apptest.Response
		pageQueries := harness.CountQueries(func() {
			firstPage = harness.Call(http.MethodGet, "/api/products?limit=25", company.OwnerToken, nil)
		})
		if len(firstPage.Items()) != 25 || firstPage.Data()["total"] != float64(200) {
			t.Fatalf("first page %d items of %v", len(firstPage.Items()), firstPage.Data()["total"])
		}
		if pageQueries > 3 {
			t.Fatalf("listing 25 products ran %d queries, want at most 3", pageQueries)
		}
		firstItemBarcodes, _ := firstPage.Items()[0].(map[string]any)["barcodes"].([]any)
		if len(firstItemBarcodes) != 1 {
			t.Fatal("barcodes were not attached to listed products")
		}

		searchCases := map[string]float64{
			"/api/products?q=item-007":          1,
			"/api/products?q=9000042":           1,
			"/api/products?q=ITEM-1":            100,
			"/api/products?category=Drinks":     100,
			"/api/products?q=nothing-like-this": 0,
		}
		for searchPath, expectedTotal := range searchCases {
			searchResult := harness.Call(http.MethodGet, searchPath, company.OwnerToken, nil)
			if searchResult.Data()["total"] != expectedTotal {
				t.Fatalf("%s total %v, want %v", searchPath, searchResult.Data()["total"], expectedTotal)
			}
		}

		categories := harness.Call(http.MethodGet, "/api/products/categories", company.OwnerToken, nil)
		categoryList, _ := categories.Body["data"].([]any)
		if len(categoryList) != 2 || categoryList[0] != "Drinks" {
			t.Fatalf("categories %v, want [Drinks Food]", categoryList)
		}

		productId := firstPage.Items()[0].(map[string]any)["id"].(string)
		crossTenantRequests := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/products/" + productId, nil},
			{http.MethodPut, "/api/products/" + productId, productBody("HIJACK", 1, nil)},
			{http.MethodDelete, "/api/products/" + productId, nil},
			{http.MethodPost, "/api/products/" + productId + "/restore", nil},
			{http.MethodGet, "/api/products/" + productId + "/variants", nil},
			{http.MethodGet, "/api/products/" + productId + "/addons", nil},
			{http.MethodPost, "/api/products/" + productId + "/addons", map[string]any{"name": "Ice", "price": 100}},
		}
		for _, crossRequest := range crossTenantRequests {
			crossResponse := harness.Call(crossRequest.method, crossRequest.path, otherCompany.OwnerToken, crossRequest.body)
			if crossResponse.Status != http.StatusNotFound {
				t.Fatalf("%s %s across tenants returned %d, want 404", crossRequest.method, crossRequest.path, crossResponse.Status)
			}
		}
		otherCompanyList := harness.Call(http.MethodGet, "/api/products", otherCompany.OwnerToken, nil)
		if otherCompanyList.Data()["total"] != float64(0) {
			t.Fatal("another company's products leaked into the list")
		}

		viewerRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Viewer", "permission_ids": []string{"products:view"}})
		harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{
			"name": "Viewer", "email": "viewer@bigcatalog.test", "password": "viewer-password", "role_id": viewerRole.Data()["id"],
		})
		viewerToken := harness.MustLogin("viewer@bigcatalog.test", "viewer-password")
		viewerCreate := harness.Call(http.MethodPost, "/api/products", viewerToken, productBody("NOPE", 1, nil))
		viewerArchive := harness.Call(http.MethodDelete, "/api/products/"+productId, viewerToken, nil)
		viewerList := harness.Call(http.MethodGet, "/api/products", viewerToken, nil)
		if viewerCreate.Status != http.StatusForbidden || viewerArchive.Status != http.StatusForbidden || viewerList.Status != http.StatusOK {
			t.Fatalf("viewer create/archive/list returned %d/%d/%d, want 403/403/200", viewerCreate.Status, viewerArchive.Status, viewerList.Status)
		}
	})
}

func TestProductAddonsAndImages(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Coffee Shop", "owner@coffee.test")
		coffee := createProduct(t, harness, company.OwnerToken, productBody("COFFEE", 3000, nil))
		coffeeId := coffee["id"].(string)

		extraShot := harness.Call(http.MethodPost, "/api/products/"+coffeeId+"/addons", company.OwnerToken, map[string]any{"name": "Extra shot", "price": 500})
		duplicateAddon := harness.Call(http.MethodPost, "/api/products/"+coffeeId+"/addons", company.OwnerToken, map[string]any{"name": "Extra shot", "price": 700})
		if extraShot.Status != http.StatusCreated || duplicateAddon.Status != http.StatusConflict {
			t.Fatalf("add-on create/duplicate returned %d/%d, want 201/409", extraShot.Status, duplicateAddon.Status)
		}
		addonId := extraShot.Data()["id"].(string)
		updatedAddon := harness.Call(http.MethodPut, "/api/addons/"+addonId, company.OwnerToken, map[string]any{"name": "Double shot", "price": 800, "is_active": false})
		if updatedAddon.Data()["name"] != "Double shot" || updatedAddon.Data()["is_active"] != false {
			t.Fatalf("add-on update: %v", updatedAddon.Data())
		}
		addonList := harness.Call(http.MethodGet, "/api/products/"+coffeeId+"/addons", company.OwnerToken, nil)
		listedAddons, _ := addonList.Body["data"].([]any)
		if len(listedAddons) != 1 {
			t.Fatalf("expected 1 add-on, got %d", len(listedAddons))
		}
		deleteAddon := harness.Call(http.MethodDelete, "/api/addons/"+addonId, company.OwnerToken, nil)
		deleteAgain := harness.Call(http.MethodDelete, "/api/addons/"+addonId, company.OwnerToken, nil)
		if deleteAddon.Status != http.StatusOK || deleteAgain.Status != http.StatusNotFound {
			t.Fatalf("add-on delete/repeat returned %d/%d, want 200/404", deleteAddon.Status, deleteAgain.Status)
		}

		picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
		encodedPicture := &bytes.Buffer{}
		png.Encode(encodedPicture, picture)

		fakeImage := harness.Upload("/api/products/"+coffeeId+"/image", company.OwnerToken, "image", "photo.png", []byte("not a picture"))
		if fakeImage.Status != http.StatusBadRequest {
			t.Fatalf("fake image returned %d, want 400", fakeImage.Status)
		}
		realImage := harness.Upload("/api/products/"+coffeeId+"/image", company.OwnerToken, "image", "photo.png", encodedPicture.Bytes())
		imageUrl, _ := realImage.Data()["image_url"].(string)
		if realImage.Status != http.StatusOK || !strings.HasPrefix(imageUrl, "/api/media/products/"+company.Id.String()+"/") {
			t.Fatalf("image upload returned %d with url %q", realImage.Status, imageUrl)
		}
		servedImage := harness.Call(http.MethodGet, imageUrl, "", nil)
		if servedImage.Status != http.StatusOK || !bytes.Equal(servedImage.Raw, encodedPicture.Bytes()) {
			t.Fatalf("serving the product image returned %d", servedImage.Status)
		}
	})
}
