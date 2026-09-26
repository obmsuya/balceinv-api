package repository

import (
	"testing"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openCatalogTestDatabase(t *testing.T) *CatalogRepository {
	database, openError := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{Logger: logger.Discard})
	if openError != nil {
		t.Fatalf("open database: %v", openError)
	}
	migrateError := database.AutoMigrate(&models.CatalogProduct{})
	if migrateError != nil {
		t.Fatalf("migrate: %v", migrateError)
	}
	return NewCatalogRepository(database)
}

func TestCatalogRepositorySavesRowsWithEmptyOptionalFields(t *testing.T) {
	catalogRepository := openCatalogTestDatabase(t)
	category := "Pain relief"
	firstList := []models.CatalogProduct{
		{BusinessType: "pharmacy", Name: "Paracetamol 500mg", Category: &category, Unit: "strip", SKUPrefix: "PARA", Metadata: models.JSONMap{"strength": "500mg"}},
		{BusinessType: "pharmacy", Name: "ORS Sachet", Unit: "sachet", SKUPrefix: "GEN"},
	}
	replaceError := catalogRepository.Replace("pharmacy", firstList)
	if replaceError != nil {
		t.Fatalf("replace: %v", replaceError)
	}

	secondList := []models.CatalogProduct{
		{BusinessType: "pharmacy", Name: "  ors   sachet ", Unit: "box", SKUPrefix: "ORS", DefaultPrice: 500},
		{BusinessType: "pharmacy", Name: "Zinc 20mg", Unit: "strip", SKUPrefix: "GEN"},
	}
	mergeResult, mergeError := catalogRepository.Merge("pharmacy", secondList)
	if mergeError != nil {
		t.Fatalf("merge: %v", mergeError)
	}
	if mergeResult.Added != 1 || mergeResult.Updated != 1 {
		t.Fatalf("merge result = %+v, want 1 added 1 updated", mergeResult)
	}

	savedProducts, findError := catalogRepository.FindByBusinessType("pharmacy")
	if findError != nil {
		t.Fatalf("find: %v", findError)
	}
	if len(savedProducts) != 3 {
		t.Fatalf("saved = %d, want 3", len(savedProducts))
	}
	for _, savedProduct := range savedProducts {
		if CatalogNameKey(savedProduct.Name) == "ors sachet" && (savedProduct.Unit != "box" || savedProduct.DefaultPrice != 500) {
			t.Fatalf("ORS was not updated: %+v", savedProduct)
		}
		if savedProduct.Name == "Paracetamol 500mg" && savedProduct.Metadata["strength"] != "500mg" {
			t.Fatalf("metadata lost: %+v", savedProduct)
		}
	}

	removedCount, deleteError := catalogRepository.DeleteByBusinessType("pharmacy")
	if deleteError != nil || removedCount != 3 {
		t.Fatalf("delete = %d, %v", removedCount, deleteError)
	}
}
