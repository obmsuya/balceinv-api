package models

import "time"

type Company struct {
    ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
    Name         string    `gorm:"not null"                 json:"name"`
    BusinessType string    `gorm:"not null"                 json:"business_type"`
    Phone        *string   `gorm:"default:null"             json:"phone"`
    Address      *string   `gorm:"default:null"             json:"address"`
    TIN          *string   `gorm:"default:null"             json:"tin"`
    Logo         *string   `gorm:"default:null"             json:"logo"`
    ReceiptHeader *string  `gorm:"column:receipt_header;default:null" json:"receipt_header"`
    ReceiptFooter *string  `gorm:"column:receipt_footer;default:null" json:"receipt_footer"`
    PrimaryColor *string   `gorm:"column:primary_color;default:#3b82f6" json:"primary_color"`
    IsSeeded     bool      `gorm:"column:is_seeded;default:false" json:"is_seeded"`
    CreatedAt    time.Time `gorm:"autoCreateTime"           json:"created_at"`
    UpdatedAt    time.Time `gorm:"autoUpdateTime"           json:"updated_at"`

    Settings *Settings `gorm:"foreignKey:CompanyID" json:"settings,omitempty"`
    Users    []User    `gorm:"foreignKey:CompanyID" json:"users,omitempty"`
}

func (Company) TableName() string { return "companies" }