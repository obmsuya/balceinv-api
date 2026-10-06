package sales

import (
	"math/big"

	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/google/uuid"
)

const basisPointsPerWhole = 10000

const (
	ManualDiscountPercent  = "percent"
	ManualDiscountAmount   = "amount"
	TillDiscountPermission = "till_discounts:create"
)

type PricingProduct struct {
	Id             uuid.UUID
	Name           string
	VariantLabel   string
	Sku            string
	Unit           string
	Price          int64
	CostPrice      int64
	WholesalePrice *int64
	WholesaleMin   int
	InStock        int
}

type PricingAddon struct {
	Id        uuid.UUID
	ProductId uuid.UUID
	Name      string
	Price     int64
}

type ManualDiscount struct {
	Kind  string
	Value int64
}

type PricingLine struct {
	Product        PricingProduct
	Quantity       int
	Addons         []PricingAddon
	ManualDiscount *ManualDiscount
}

type PricedLine struct {
	Product         PricingProduct
	Quantity        int
	UnitPrice       int64
	IsWholesale     bool
	Addons          []PricingAddon
	AddonsUnitTotal int64
	DiscountId      *uuid.UUID
	DiscountName    *string
	DiscountAmount  int64
	ManualDiscount  int64
	LineTotal       int64
}

func (pricedLine PricedLine) GrossTotal() int64 {
	return int64(pricedLine.Quantity) * (pricedLine.UnitPrice + pricedLine.AddonsUnitTotal)
}

type PricedSale struct {
	Lines              []PricedLine
	Subtotal           int64
	DiscountTotal      int64
	Total              int64
	TaxTotal           int64
	TaxRateBasisPoints int
}

func PriceSale(pricingLines []PricingLine, applicableDiscounts []discounts.Discount, taxRateBasisPoints int) PricedSale {
	pricedSale := PricedSale{
		Lines:              make([]PricedLine, 0, len(pricingLines)),
		TaxRateBasisPoints: taxRateBasisPoints,
	}

	for _, pricingLine := range pricingLines {
		pricedLine := priceLine(pricingLine, applicableDiscounts)
		pricedSale.Lines = append(pricedSale.Lines, pricedLine)
		pricedSale.Subtotal += pricedLine.LineTotal + pricedLine.DiscountAmount
		pricedSale.DiscountTotal += pricedLine.DiscountAmount
	}

	pricedSale.Total = pricedSale.Subtotal - pricedSale.DiscountTotal
	pricedSale.TaxTotal = IncludedTax(pricedSale.Total, taxRateBasisPoints)
	return pricedSale
}

func IncludedTax(taxInclusiveTotal int64, taxRateBasisPoints int) int64 {
	if taxRateBasisPoints <= 0 || taxInclusiveTotal <= 0 {
		return 0
	}
	return multiplyDivideRoundHalfUp(taxInclusiveTotal, int64(taxRateBasisPoints), int64(basisPointsPerWhole+taxRateBasisPoints))
}

func priceLine(pricingLine PricingLine, applicableDiscounts []discounts.Discount) PricedLine {
	product := pricingLine.Product
	quantity := int64(pricingLine.Quantity)

	isWholesale := product.WholesalePrice != nil && product.WholesaleMin > 0 && pricingLine.Quantity >= product.WholesaleMin
	unitPrice := product.Price
	if isWholesale {
		unitPrice = *product.WholesalePrice
	}

	addonsUnitTotal := int64(0)
	for _, addon := range pricingLine.Addons {
		addonsUnitTotal += addon.Price
	}

	pricedLine := PricedLine{
		Product:         product,
		Quantity:        pricingLine.Quantity,
		UnitPrice:       unitPrice,
		IsWholesale:     isWholesale,
		Addons:          pricingLine.Addons,
		AddonsUnitTotal: addonsUnitTotal,
	}

	if !isWholesale {
		bestDiscount, bestAmount := bestDiscountFor(product.Id, unitPrice, quantity, applicableDiscounts)
		if bestDiscount != nil {
			discountId := bestDiscount.Id
			discountName := bestDiscount.Name
			pricedLine.DiscountId = &discountId
			pricedLine.DiscountName = &discountName
			pricedLine.DiscountAmount = bestAmount
		}
	}

	if pricingLine.ManualDiscount != nil {
		amountLeftToDiscount := pricedLine.GrossTotal() - pricedLine.DiscountAmount
		pricedLine.ManualDiscount = min(manualDiscountAmount(*pricingLine.ManualDiscount, pricedLine.GrossTotal()), amountLeftToDiscount)
		pricedLine.DiscountAmount += pricedLine.ManualDiscount
	}

	pricedLine.LineTotal = pricedLine.GrossTotal() - pricedLine.DiscountAmount
	return pricedLine
}

func manualDiscountAmount(manualDiscount ManualDiscount, grossTotal int64) int64 {
	if manualDiscount.Kind == ManualDiscountPercent {
		return multiplyDivideRoundHalfUp(grossTotal, min(manualDiscount.Value, basisPointsPerWhole), basisPointsPerWhole)
	}
	return manualDiscount.Value
}

func bestDiscountFor(productId uuid.UUID, unitPrice int64, quantity int64, applicableDiscounts []discounts.Discount) (*discounts.Discount, int64) {
	var bestDiscount *discounts.Discount
	bestAmount := int64(0)

	for discountIndex := range applicableDiscounts {
		candidate := &applicableDiscounts[discountIndex]
		isForThisProduct := candidate.ProductId == nil || *candidate.ProductId == productId
		if !isForThisProduct {
			continue
		}

		candidateAmount := discountAmount(candidate.Kind, candidate.Value, unitPrice, quantity)
		isBetter := candidateAmount > bestAmount ||
			(candidateAmount == bestAmount && candidateAmount > 0 && bestDiscount != nil && prefersCandidate(*candidate, *bestDiscount))
		if isBetter {
			bestDiscount = candidate
			bestAmount = candidateAmount
		}
	}

	return bestDiscount, bestAmount
}

func discountAmount(kind string, value int64, unitPrice int64, quantity int64) int64 {
	if kind == discounts.KindPercent {
		return multiplyDivideRoundHalfUp(unitPrice*quantity, value, basisPointsPerWhole)
	}
	perUnitAmount := min(value, unitPrice)
	return perUnitAmount * quantity
}

func prefersCandidate(candidate discounts.Discount, current discounts.Discount) bool {
	candidateIsSpecific := candidate.ProductId != nil
	currentIsSpecific := current.ProductId != nil
	if candidateIsSpecific != currentIsSpecific {
		return candidateIsSpecific
	}
	return candidate.Id.String() < current.Id.String()
}

func multiplyDivideRoundHalfUp(value int64, multiplier int64, divisor int64) int64 {
	numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(multiplier))
	numerator.Mul(numerator, big.NewInt(2))
	numerator.Add(numerator, big.NewInt(divisor))
	denominator := big.NewInt(divisor * 2)
	return new(big.Int).Quo(numerator, denominator).Int64()
}
