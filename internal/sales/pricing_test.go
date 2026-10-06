package sales

import (
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/google/uuid"
)

func product(price int64, wholesalePrice *int64, wholesaleMin int) PricingProduct {
	return PricingProduct{Id: uuid.Must(uuid.NewV7()), Name: "Item", Price: price, WholesalePrice: wholesalePrice, WholesaleMin: wholesaleMin}
}

func discount(kind string, value int64, productId *uuid.UUID) discounts.Discount {
	return discounts.Discount{Id: uuid.Must(uuid.NewV7()), Name: kind, Kind: kind, Value: value, ProductId: productId, StartsAt: time.Now(), EndsAt: time.Now()}
}

func TestIncludedTaxRoundsHalfUp(t *testing.T) {
	cases := []struct {
		total int64
		rate  int
		want  int64
	}{
		{1, 1800, 0},
		{3, 1800, 0},
		{4, 1800, 1},
		{100, 1800, 15},
		{118, 1800, 18},
		{1000, 1800, 153},
		{999, 1800, 152},
		{59, 1800, 9},
		{1180, 1800, 180},
		{0, 1800, 0},
		{5000, 0, 0},
		{105, 500, 5},
		{21, 500, 1},
	}
	for _, taxCase := range cases {
		if got := IncludedTax(taxCase.total, taxCase.rate); got != taxCase.want {
			t.Fatalf("IncludedTax(%d, %d) = %d, want %d", taxCase.total, taxCase.rate, got, taxCase.want)
		}
	}
}

func TestWholesaleStartsExactlyAtTheMinimum(t *testing.T) {
	wholesalePrice := int64(850)
	soda := product(1000, &wholesalePrice, 10)

	belowMinimum := PriceSale([]PricingLine{{Product: soda, Quantity: 9}}, nil, 0)
	atMinimum := PriceSale([]PricingLine{{Product: soda, Quantity: 10}}, nil, 0)
	if belowMinimum.Lines[0].IsWholesale || belowMinimum.Total != 9000 {
		t.Fatalf("9 units priced as %+v", belowMinimum.Lines[0])
	}
	if !atMinimum.Lines[0].IsWholesale || atMinimum.Total != 8500 {
		t.Fatalf("10 units priced as %+v", atMinimum.Lines[0])
	}
}

func TestBestDiscountWinsAndSkipsWholesale(t *testing.T) {
	wholesalePrice := int64(700)
	coffee := product(3000, &wholesalePrice, 50)
	otherProductId := uuid.Must(uuid.NewV7())
	applicable := []discounts.Discount{
		discount(discounts.KindPercent, 1000, nil),
		discount(discounts.KindFixed, 500, &coffee.Id),
		discount(discounts.KindPercent, 9000, &otherProductId),
	}

	retail := PriceSale([]PricingLine{{Product: coffee, Quantity: 2}}, applicable, 0)
	retailLine := retail.Lines[0]
	if retailLine.DiscountAmount != 1000 || *retailLine.DiscountName != discounts.KindFixed || retail.Total != 5000 || retail.Subtotal != 6000 {
		t.Fatalf("retail line %+v, sale %+v", retailLine, retail)
	}

	wholesale := PriceSale([]PricingLine{{Product: coffee, Quantity: 50}}, applicable, 0)
	if wholesale.Lines[0].DiscountAmount != 0 || wholesale.Lines[0].DiscountId != nil || wholesale.Total != 35000 {
		t.Fatalf("wholesale line took a discount: %+v", wholesale.Lines[0])
	}
}

func TestFixedDiscountNeverGoesBelowZeroAndPercentRounds(t *testing.T) {
	sweet := product(300, nil, 0)
	bigFixed := PriceSale([]PricingLine{{Product: sweet, Quantity: 3}}, []discounts.Discount{discount(discounts.KindFixed, 1000, nil)}, 1800)
	if bigFixed.Total != 0 || bigFixed.DiscountTotal != 900 || bigFixed.TaxTotal != 0 {
		t.Fatalf("fixed discount above the price gave %+v", bigFixed)
	}

	oneUnit := product(1, nil, 0)
	awkward := PriceSale([]PricingLine{{Product: oneUnit, Quantity: 7}}, []discounts.Discount{discount(discounts.KindPercent, 1250, nil)}, 1800)
	if awkward.DiscountTotal != 1 || awkward.Total != 6 || awkward.TaxTotal != 1 {
		t.Fatalf("7 × 1-unit items at 12.5%% off and 18%% tax gave %+v", awkward)
	}
}

func TestAddonsAreChargedPerUnitAndNotDiscounted(t *testing.T) {
	coffee := product(3000, nil, 0)
	shot := PricingAddon{Id: uuid.Must(uuid.NewV7()), ProductId: coffee.Id, Name: "Extra shot", Price: 500}
	oatMilk := PricingAddon{Id: uuid.Must(uuid.NewV7()), ProductId: coffee.Id, Name: "Oat milk", Price: 250}

	priced := PriceSale([]PricingLine{{Product: coffee, Quantity: 2, Addons: []PricingAddon{shot, oatMilk}}}, []discounts.Discount{discount(discounts.KindPercent, 1000, nil)}, 1800)
	line := priced.Lines[0]
	if line.AddonsUnitTotal != 750 || line.DiscountAmount != 600 || line.LineTotal != 6900 || priced.Subtotal != 7500 || priced.TaxTotal != 1053 {
		t.Fatalf("coffee with add-ons priced as %+v, sale %+v", line, priced)
	}
}

func TestLargeAmountsDoNotOverflow(t *testing.T) {
	pricey := product(1_000_000_000_000, nil, 0)
	priced := PriceSale([]PricingLine{{Product: pricey, Quantity: 100_000}}, []discounts.Discount{discount(discounts.KindPercent, 3333, nil)}, 1800)
	if priced.DiscountTotal != 33_330_000_000_000_000 || priced.Total != 66_670_000_000_000_000 {
		t.Fatalf("large sale priced as %+v", priced)
	}
}

func TestCashierDiscountStacksOnTheAutomaticOneAndNeverGoesBelowZero(t *testing.T) {
	coffee := product(3000, nil, 0)
	extraShot := PricingAddon{Id: uuid.Must(uuid.NewV7()), ProductId: coffee.Id, Name: "Extra shot", Price: 500}
	coffeeHour := discount(discounts.KindPercent, 1000, &coffee.Id)
	priceWith := func(manualDiscount *ManualDiscount, applicableDiscounts []discounts.Discount) PricedLine {
		pricingLine := PricingLine{Product: coffee, Quantity: 2, Addons: []PricingAddon{extraShot}, ManualDiscount: manualDiscount}
		return PriceSale([]PricingLine{pricingLine}, applicableDiscounts, 0).Lines[0]
	}

	tenPercent := priceWith(&ManualDiscount{Kind: ManualDiscountPercent, Value: 1000}, nil)
	if tenPercent.ManualDiscount != 700 || tenPercent.DiscountAmount != 700 || tenPercent.LineTotal != 6300 {
		t.Fatalf("10%% off 2 coffees with a shot priced as %+v", tenPercent)
	}

	stacked := priceWith(&ManualDiscount{Kind: ManualDiscountPercent, Value: 1000}, []discounts.Discount{coffeeHour})
	if stacked.ManualDiscount != 700 || stacked.DiscountAmount != 1300 || stacked.LineTotal != 5700 || *stacked.DiscountName != coffeeHour.Name {
		t.Fatalf("a cashier discount on top of coffee hour priced as %+v", stacked)
	}

	tooBig := priceWith(&ManualDiscount{Kind: ManualDiscountAmount, Value: 100000}, []discounts.Discount{coffeeHour})
	if tooBig.ManualDiscount != 6400 || tooBig.LineTotal != 0 {
		t.Fatalf("an amount larger than the line priced as %+v", tooBig)
	}

	overHundredPercent := priceWith(&ManualDiscount{Kind: ManualDiscountPercent, Value: 25000}, nil)
	if overHundredPercent.ManualDiscount != 7000 || overHundredPercent.LineTotal != 0 {
		t.Fatalf("more than 100%% priced as %+v", overHundredPercent)
	}

}
