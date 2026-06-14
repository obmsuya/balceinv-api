package database

import (
	"log"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	if _, err = sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		log.Printf("Warning: could not set WAL mode: %v", err)
	}

	if _, err = sqlDB.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		log.Printf("Warning: could not enable foreign keys: %v", err)
	}

	log.Println("Database connected:", dbPath)
	return db, nil
}