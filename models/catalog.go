package models

import (
    "database/sql/driver"
    "encoding/json"
    "errors"
)

type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
    if j == nil {
        return nil, nil
    }
    b, err := json.Marshal(j)
    return string(b), err
}

func (j *JSONMap) Scan(value interface{}) error {
    if value == nil {
        *j = nil
        return nil
    }
    bytes, ok := value.(string)
    if !ok {
        return errors.New("type assertion to string failed")
    }
    return json.Unmarshal([]byte(bytes), j)
}

type CatalogProduct struct {
    ID           uint    `gorm:"primaryKey;autoIncrement" json:"id"`
    BusinessType string  `gorm:"not null;index"           json:"business_type"`
    Name         string  `gorm:"not null"                 json:"name"`
    Category     *string `gorm:"default:null"             json:"category"`
    SubCategory  *string `gorm:"column:sub_category;default:null" json:"sub_category"`
    Unit         string  `gorm:"default:pcs"              json:"unit"`
    SKUPrefix    string  `gorm:"column:sku_prefix;default:GEN" json:"sku_prefix"`
    DefaultPrice float64 `gorm:"column:default_price;default:0" json:"default_price"`
    Metadata     JSONMap `gorm:"type:text;default:null"   json:"metadata"`
}

func (CatalogProduct) TableName() string { return "catalog_products" }