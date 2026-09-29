package catalog_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

var teamHeaders = map[string]string{httpx.SupportPasscodeHeader: apptest.SupportPasscode}

func importList(harness *apptest.Harness, sessionToken string, businessType string, mode string, fileName string, fileBytes []byte) apptest.Response {
	formValues := map[string]string{
		"business_type": businessType,
		"mode":          mode,
	}
	return harness.UploadForm("/api/catalog/team/import", sessionToken, "file", fileName, fileBytes, formValues, teamHeaders)
}

func listNames(listResponse apptest.Response) []string {
	rawItems, _ := listResponse.Body["data"].([]any)
	names := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		names = append(names, rawItem.(map[string]any)["name"].(string))
	}
	return names
}

func TestTeamToolsMaintainCommonProductLists(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Catalog Shop", "owner@catalog.test")

		emptyList := harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil)
		if emptyList.Status != http.StatusOK || len(listNames(emptyList)) != 0 {
			t.Fatalf("fresh catalog returned %d %v", emptyList.Status, emptyList.Body)
		}

		noPasscode := harness.Call(http.MethodGet, "/api/catalog/team/summary", company.OwnerToken, nil)
		wrongPasscode := harness.Send(http.MethodGet, "/api/catalog/team/summary", company.OwnerToken, nil, map[string]string{httpx.SupportPasscodeHeader: "guess"})
		if noPasscode.Status != http.StatusForbidden || wrongPasscode.Status != http.StatusForbidden {
			t.Fatalf("team tools without the passcode returned %d and %d, want 403", noPasscode.Status, wrongPasscode.Status)
		}
		signedOut := harness.Send(http.MethodGet, "/api/catalog/team/summary", "", nil, teamHeaders)
		if signedOut.Status != http.StatusUnauthorized {
			t.Fatalf("team tools while signed out returned %d, want 401", signedOut.Status)
		}

		generalCsv := "name,category,unit,price,Brand\n" +
			"Sugar 1kg,Groceries,kg,\"3,500\",Kilombero\n" +
			"Rice 1kg,Groceries,kg,2800,\n" +
			"Soap bar,Cleaning,pcs,1000,Jamaa\n" +
			",Groceries,kg,100,\n"
		firstImport := importList(harness, company.OwnerToken, "general", "merge", "general.csv", []byte(generalCsv))
		firstResult := firstImport.Data()
		if firstImport.Status != http.StatusOK || firstResult["added"] != float64(3) || firstResult["skipped"] != float64(1) || firstResult["total_in_list"] != float64(3) {
			t.Fatalf("first import returned %d %v", firstImport.Status, firstImport.Body)
		}

		companyList := harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil)
		if strings.Join(listNames(companyList), "|") != "Rice 1kg|Soap bar|Sugar 1kg" {
			t.Fatalf("company catalog = %v", listNames(companyList))
		}
		sugar := companyList.Body["data"].([]any)[2].(map[string]any)
		sugarMetadata, _ := sugar["metadata"].(map[string]any)
		if sugar["default_price"] != float64(3500) || sugarMetadata["Brand"] != "Kilombero" {
			t.Fatalf("sugar stored as %v", sugar)
		}

		mergeCsv := "name,price\nSUGAR  1KG,3700\nSalt 500g,600\n"
		mergeImport := importList(harness, company.OwnerToken, "general", "merge", "merge.csv", []byte(mergeCsv))
		mergeResult := mergeImport.Data()
		if mergeImport.Status != http.StatusOK || mergeResult["added"] != float64(1) || mergeResult["updated"] != float64(1) || mergeResult["total_in_list"] != float64(4) {
			t.Fatalf("merge import returned %d %v", mergeImport.Status, mergeImport.Body)
		}

		templateResponse := harness.Send(http.MethodGet, "/api/catalog/team/template", company.OwnerToken, nil, teamHeaders)
		if templateResponse.Status != http.StatusOK || !strings.Contains(templateResponse.Headers.Get("Content-Type"), "spreadsheetml") {
			t.Fatalf("template returned %d %s", templateResponse.Status, templateResponse.Headers.Get("Content-Type"))
		}
		pharmacyImport := importList(harness, company.OwnerToken, "pharmacy", "replace", "pharmacy.xlsx", templateResponse.Raw)
		if pharmacyImport.Status != http.StatusOK || pharmacyImport.Data()["total_in_list"] != float64(3) {
			t.Fatalf("template import returned %d %v", pharmacyImport.Status, pharmacyImport.Body)
		}
		replaceImport := importList(harness, company.OwnerToken, "pharmacy", "replace", "one.csv", []byte("name\nPanadol\n"))
		if replaceImport.Status != http.StatusOK || replaceImport.Data()["total_in_list"] != float64(1) {
			t.Fatalf("replace import returned %d %v", replaceImport.Status, replaceImport.Body)
		}

		summary := harness.Send(http.MethodGet, "/api/catalog/team/summary", company.OwnerToken, nil, teamHeaders).Data()
		summaryCounts := fmt.Sprint(summary["counts"])
		if summary["company_business_type"] != "general" || summaryCounts != "[map[business_type:general count:4] map[business_type:pharmacy count:1]]" {
			t.Fatalf("summary = %v", summary)
		}
		if len(listNames(harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil))) != 4 {
			t.Fatal("another business type's list leaked into the company catalog")
		}
		pharmacyItems := harness.Send(http.MethodGet, "/api/catalog/team/items?business_type=pharmacy", company.OwnerToken, nil, teamHeaders)
		if strings.Join(listNames(pharmacyItems), "|") != "Panadol" {
			t.Fatalf("pharmacy items = %v", listNames(pharmacyItems))
		}

		rejectedImport := importList(harness, company.OwnerToken, "general", "replace", "bad.csv", []byte("name,price\n,100\nBroken,abc\n"))
		if rejectedImport.Status != http.StatusUnprocessableEntity || rejectedImport.Code() != "import_rejected" || rejectedImport.Data()["problems_total"] != float64(2) {
			t.Fatalf("all-bad import returned %d %v", rejectedImport.Status, rejectedImport.Body)
		}
		if len(listNames(harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil))) != 4 {
			t.Fatal("a rejected replace import still cleared the list")
		}

		invalidRequests := []struct {
			businessType string
			mode         string
			fileName     string
			wantCode     string
		}{
			{"General!", "merge", "a.csv", "invalid_request"},
			{"general", "overwrite", "a.csv", "invalid_request"},
			{"general", "merge", "a.txt", "invalid_import"},
			{"general", "merge", "a.csv", "invalid_import"},
		}
		for _, invalidRequest := range invalidRequests {
			fileText := "name\nThing\n"
			if invalidRequest.wantCode == "invalid_import" && strings.HasSuffix(invalidRequest.fileName, ".csv") {
				fileText = "price\n100\n"
			}
			invalidImport := importList(harness, company.OwnerToken, invalidRequest.businessType, invalidRequest.mode, invalidRequest.fileName, []byte(fileText))
			if invalidImport.Status != http.StatusBadRequest || invalidImport.Code() != invalidRequest.wantCode {
				t.Fatalf("%+v returned %d %s", invalidRequest, invalidImport.Status, invalidImport.Code())
			}
		}

		clearResponse := harness.Send(http.MethodDelete, "/api/catalog/team?business_type=general", company.OwnerToken, nil, teamHeaders)
		if clearResponse.Status != http.StatusOK || clearResponse.Data()["removed"] != float64(4) {
			t.Fatalf("clear returned %d %v", clearResponse.Status, clearResponse.Body)
		}
		if len(listNames(harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil))) != 0 {
			t.Fatal("cleared list still shows products")
		}
	})
}

func TestTeamToolsHandleLargeListsAndLockOutGuessing(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Bulk Catalog", "owner@bulk-catalog.test")

		largeCsv := strings.Builder{}
		largeCsv.WriteString("name,category,price\n")
		for rowNumber := 1; rowNumber <= 1200; rowNumber++ {
			fmt.Fprintf(&largeCsv, "Common item %04d,Bulk,%d\n", rowNumber, rowNumber*10)
		}
		largeImport := importList(harness, company.OwnerToken, "general", "merge", "large.csv", []byte(largeCsv.String()))
		if largeImport.Status != http.StatusOK || largeImport.Data()["added"] != float64(1200) {
			t.Fatalf("large import returned %d %v", largeImport.Status, largeImport.Body)
		}

		listQueries := harness.CountQueries(func() {
			harness.Call(http.MethodGet, "/api/catalog", company.OwnerToken, nil)
		})
		if listQueries > 6 {
			t.Fatalf("listing the catalog took %d queries", listQueries)
		}

		for attempt := 1; attempt <= 5; attempt++ {
			harness.Send(http.MethodGet, "/api/catalog/team/summary", company.OwnerToken, nil, map[string]string{httpx.SupportPasscodeHeader: "guess"})
		}
		lockedOut := harness.Send(http.MethodGet, "/api/catalog/team/summary", company.OwnerToken, nil, teamHeaders)
		if lockedOut.Status != http.StatusTooManyRequests {
			t.Fatalf("correct passcode during lockout returned %d, want 429", lockedOut.Status)
		}
	})
}
