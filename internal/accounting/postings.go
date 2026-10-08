package accounting

import (
	"context"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type SalePosting struct {
	CompanyId    uuid.UUID
	SaleId       uuid.UUID
	ShopId       uuid.UUID
	SoldAt       time.Time
	Reference    string
	Total        int64
	TaxTotal     int64
	ChangeGiven  int64
	PaidByMethod map[string]int64
	CostTotal    int64
	CustomerId   *uuid.UUID
	UserId       *uuid.UUID
}

type StockMovementPosting struct {
	CompanyId  uuid.UUID
	MovementId uuid.UUID
	ShopId     uuid.UUID
	Reason     string
	Change     int
	UnitCost   int64
	MovedAt    time.Time
	Reference  *string
	UserId     *uuid.UUID
}

type TransferPosting struct {
	CompanyId   uuid.UUID
	TransferId  uuid.UUID
	FromShopId  uuid.UUID
	ToShopId    uuid.UUID
	ValueAtCost int64
	SentAt      time.Time
	UserId      *uuid.UUID
}

type PurchasePosting struct {
	CompanyId      uuid.UUID
	PurchaseId     uuid.UUID
	ShopId         uuid.UUID
	ReceivedAt     time.Time
	NetCost        int64
	Vat            int64
	PaidByMethod   map[string]int64
	OwedToSupplier int64
	SupplierId     *uuid.UUID
	Reference      string
	UserId         *uuid.UUID
}

type SupplierReturnPosting struct {
	CompanyId          uuid.UUID
	ReturnId           uuid.UUID
	ShopId             uuid.UUID
	ReturnedAt         time.Time
	NetCost            int64
	Vat                int64
	RefundByMethod     map[string]int64
	CreditFromSupplier int64
	SupplierId         *uuid.UUID
	Reference          string
	UserId             *uuid.UUID
}

type PaymentPosting struct {
	CompanyId uuid.UUID
	SourceId  uuid.UUID
	PartyId   *uuid.UUID
	ShopId    *uuid.UUID
	PaidAt    time.Time
	Method    string
	Amount    int64
	Reference string
	UserId    *uuid.UUID
}

type ReversalPosting struct {
	CompanyId          uuid.UUID
	OriginalSourceType string
	OriginalSourceId   uuid.UUID
	SourceType         string
	ReversedAt         time.Time
	Reason             string
	UserId             *uuid.UUID
}

type SaleRefundPosting struct {
	CompanyId  uuid.UUID
	RefundId   uuid.UUID
	ShopId     uuid.UUID
	RefundedAt time.Time
	Method     string
	Amount     int64
	TaxAmount  int64
	CostAmount int64
	Restocked  bool
	CustomerId *uuid.UUID
	Reference  string
	UserId     *uuid.UUID
}

func (ledger *Ledger) PostSaleRefund(ctx context.Context, querier database.Querier, posting SaleRefundPosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}

	shopId := posting.ShopId
	refundLines, methodError := methodLines(map[string]int64{posting.Method: posting.Amount}, false, &shopId)
	if methodError != nil {
		return nil, methodError
	}
	vatReturned := int64(0)
	if books.VatRegistered {
		vatReturned = posting.TaxAmount
	}
	refundLines = append(refundLines,
		keyedLine{Key: KeySales, Debit: posting.Amount - vatReturned, ShopId: &shopId},
		keyedLine{Key: KeyVatOutput, Debit: vatReturned, ShopId: &shopId},
	)
	if posting.Restocked {
		refundLines = append(refundLines,
			keyedLine{Key: KeyInventory, Debit: posting.CostAmount, ShopId: &shopId},
			keyedLine{Key: KeyCogs, Credit: posting.CostAmount, ShopId: &shopId},
		)
	}

	partyType, partyId := partyOf(PartyCustomer, posting.CustomerId)
	header := Entry{
		SourceType: SourceSaleVoid,
		SourceId:   &posting.RefundId,
		Memo:       textOrNil(posting.Reference),
		ShopId:     &shopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.RefundedAt, header, refundLines)
}

func (ledger *Ledger) PostSale(ctx context.Context, querier database.Querier, posting SalePosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postSale(ctx, querier, books, posting)
}

func (ledger *Ledger) postSale(ctx context.Context, querier database.Querier, books Books, posting SalePosting) (*PostedEntry, error) {
	shopId := posting.ShopId
	netPaid := map[string]int64{}
	for method, amount := range posting.PaidByMethod {
		netPaid[method] = amount
	}
	netPaid["cash"] -= posting.ChangeGiven
	if netPaid["cash"] == 0 {
		delete(netPaid, "cash")
	}

	saleLines, methodError := methodLines(netPaid, true, &shopId)
	if methodError != nil {
		return nil, methodError
	}

	vatCharged := int64(0)
	if books.VatRegistered {
		vatCharged = posting.TaxTotal
	}
	saleLines = append(saleLines,
		keyedLine{Key: KeySales, Credit: posting.Total - vatCharged, ShopId: &shopId},
		keyedLine{Key: KeyVatOutput, Credit: vatCharged, ShopId: &shopId},
		keyedLine{Key: KeyCogs, Debit: posting.CostTotal, ShopId: &shopId},
		keyedLine{Key: KeyInventory, Credit: posting.CostTotal, ShopId: &shopId},
	)

	partyType, partyId := partyOf(PartyCustomer, posting.CustomerId)
	header := Entry{
		SourceType: SourceSale,
		SourceId:   &posting.SaleId,
		Memo:       textOrNil(posting.Reference),
		ShopId:     &shopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.SoldAt, header, saleLines)
}

func (ledger *Ledger) PostStockMovement(ctx context.Context, querier database.Querier, posting StockMovementPosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postStockMovement(ctx, querier, books, posting)
}

func (ledger *Ledger) postStockMovement(ctx context.Context, querier database.Querier, books Books, posting StockMovementPosting) (*PostedEntry, error) {
	shopId := posting.ShopId
	quantity := int64(posting.Change)
	if quantity < 0 {
		quantity = -quantity
	}
	valueAtCost := quantity * posting.UnitCost

	movementLines := []keyedLine{
		{Key: KeyStockLosses, Debit: valueAtCost, ShopId: &shopId},
		{Key: KeyInventory, Credit: valueAtCost, ShopId: &shopId},
	}
	if posting.Change > 0 {
		counterKey := KeyStockGains
		isOwnersStock := posting.Reason == "opening" || posting.Reason == "purchase"
		if isOwnersStock {
			counterKey = KeyOwnerCapital
		}
		movementLines = []keyedLine{
			{Key: KeyInventory, Debit: valueAtCost, ShopId: &shopId},
			{Key: counterKey, Credit: valueAtCost, ShopId: &shopId},
		}
	}

	header := Entry{
		SourceType: SourceStockAdjustment,
		SourceId:   &posting.MovementId,
		Memo:       posting.Reference,
		ShopId:     &shopId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.MovedAt, header, movementLines)
}

func (ledger *Ledger) PostTransfer(ctx context.Context, querier database.Querier, posting TransferPosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postTransfer(ctx, querier, books, posting)
}

func (ledger *Ledger) postTransfer(ctx context.Context, querier database.Querier, books Books, posting TransferPosting) (*PostedEntry, error) {
	fromShopId := posting.FromShopId
	toShopId := posting.ToShopId
	transferLines := []keyedLine{
		{Key: KeyInventory, Debit: posting.ValueAtCost, ShopId: &toShopId},
		{Key: KeyInventory, Credit: posting.ValueAtCost, ShopId: &fromShopId},
	}
	header := Entry{
		SourceType: SourceStockTransfer,
		SourceId:   &posting.TransferId,
		ShopId:     &fromShopId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.SentAt, header, transferLines)
}

func (ledger *Ledger) PostPurchase(ctx context.Context, querier database.Querier, posting PurchasePosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postPurchase(ctx, querier, books, posting)
}

func (ledger *Ledger) postPurchase(ctx context.Context, querier database.Querier, books Books, posting PurchasePosting) (*PostedEntry, error) {
	shopId := posting.ShopId
	stockCost, vatReclaimable := splitVat(books, posting.NetCost, posting.Vat)
	paymentLines, methodError := methodLines(posting.PaidByMethod, false, &shopId)
	if methodError != nil {
		return nil, methodError
	}
	purchaseLines := append([]keyedLine{
		{Key: KeyInventory, Debit: stockCost, ShopId: &shopId},
		{Key: KeyVatInput, Debit: vatReclaimable, ShopId: &shopId},
		{Key: KeyPayable, Credit: posting.OwedToSupplier, ShopId: &shopId},
	}, paymentLines...)

	partyType, partyId := partyOf(PartySupplier, posting.SupplierId)
	header := Entry{
		SourceType: SourcePurchase,
		SourceId:   &posting.PurchaseId,
		Memo:       textOrNil(posting.Reference),
		ShopId:     &shopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.ReceivedAt, header, purchaseLines)
}

func (ledger *Ledger) PostSupplierReturn(ctx context.Context, querier database.Querier, posting SupplierReturnPosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postSupplierReturn(ctx, querier, books, posting)
}

func (ledger *Ledger) postSupplierReturn(ctx context.Context, querier database.Querier, books Books, posting SupplierReturnPosting) (*PostedEntry, error) {
	shopId := posting.ShopId
	stockCost, vatReclaimable := splitVat(books, posting.NetCost, posting.Vat)
	refundLines, methodError := methodLines(posting.RefundByMethod, true, &shopId)
	if methodError != nil {
		return nil, methodError
	}
	returnLines := append(refundLines,
		keyedLine{Key: KeyPayable, Debit: posting.CreditFromSupplier, ShopId: &shopId},
		keyedLine{Key: KeyInventory, Credit: stockCost, ShopId: &shopId},
		keyedLine{Key: KeyVatInput, Credit: vatReclaimable, ShopId: &shopId},
	)

	partyType, partyId := partyOf(PartySupplier, posting.SupplierId)
	header := Entry{
		SourceType: SourceSupplierReturn,
		SourceId:   &posting.ReturnId,
		Memo:       textOrNil(posting.Reference),
		ShopId:     &shopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.ReturnedAt, header, returnLines)
}

func (ledger *Ledger) PostSupplierPayment(ctx context.Context, querier database.Querier, posting PaymentPosting) (*PostedEntry, error) {
	return ledger.postPayment(ctx, querier, posting, SourceSupplierPayment)
}

func (ledger *Ledger) PostCustomerPayment(ctx context.Context, querier database.Querier, posting PaymentPosting) (*PostedEntry, error) {
	return ledger.postPayment(ctx, querier, posting, SourceCustomerPayment)
}

func (ledger *Ledger) PostOrderDeposit(ctx context.Context, querier database.Querier, posting PaymentPosting) (*PostedEntry, error) {
	return ledger.postPayment(ctx, querier, posting, SourceOrderDeposit)
}

func (ledger *Ledger) PostOrderRefund(ctx context.Context, querier database.Querier, posting PaymentPosting) (*PostedEntry, error) {
	return ledger.postPayment(ctx, querier, posting, SourceOrderRefund)
}

type paymentShape struct {
	partyType  string
	counterKey string
	isMoneyIn  bool
}

var paymentShapes = map[string]paymentShape{
	SourceSupplierPayment: {PartySupplier, KeyPayable, false},
	SourceCustomerPayment: {PartyCustomer, KeyReceivable, true},
	SourceOrderDeposit:    {PartyCustomer, KeyCustomerDeposits, true},
	SourceOrderRefund:     {PartyCustomer, KeyCustomerDeposits, false},
}

func (ledger *Ledger) postPayment(ctx context.Context, querier database.Querier, posting PaymentPosting, sourceType string) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, posting.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postPaymentWithBooks(ctx, querier, books, posting, sourceType)
}

func (ledger *Ledger) postPaymentWithBooks(ctx context.Context, querier database.Querier, books Books, posting PaymentPosting, sourceType string) (*PostedEntry, error) {
	shape := paymentShapes[sourceType]
	moneyKey, keyError := moneyKeyFor(posting.Method)
	if keyError != nil {
		return nil, keyError
	}

	paymentLines := []keyedLine{
		{Key: shape.counterKey, Debit: posting.Amount, ShopId: posting.ShopId},
		{Key: moneyKey, Credit: posting.Amount, ShopId: posting.ShopId},
	}
	if shape.isMoneyIn {
		paymentLines = []keyedLine{
			{Key: moneyKey, Debit: posting.Amount, ShopId: posting.ShopId},
			{Key: shape.counterKey, Credit: posting.Amount, ShopId: posting.ShopId},
		}
	}

	partyType, partyId := partyOf(shape.partyType, posting.PartyId)
	header := Entry{
		SourceType: sourceType,
		SourceId:   &posting.SourceId,
		Memo:       textOrNil(posting.Reference),
		ShopId:     posting.ShopId,
		PartyType:  partyType,
		PartyId:    partyId,
		CreatedBy:  posting.UserId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, posting.PaidAt, header, paymentLines)
}

func (ledger *Ledger) ReverseSource(ctx context.Context, querier database.Querier, reversal ReversalPosting) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, reversal.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.reverseSourceWithBooks(ctx, querier, books, reversal)
}

func (ledger *Ledger) reverseSourceWithBooks(ctx context.Context, querier database.Querier, books Books, reversal ReversalPosting) (*PostedEntry, error) {
	originalEntry, findError := ledger.repository.FindBySource(ctx, querier, reversal.CompanyId, reversal.OriginalSourceType, reversal.OriginalSourceId)
	if findError != nil {
		return nil, findError
	}
	if originalEntry == nil {
		return nil, nil
	}
	originalLines, linesError := ledger.repository.ListEntryLines(ctx, querier, reversal.CompanyId, originalEntry.Id)
	if linesError != nil {
		return nil, linesError
	}

	reversedEntry := Entry{
		EntryDate:       firstOpenDate(books, books.LocalDate(reversal.ReversedAt)),
		SourceType:      reversal.SourceType,
		SourceId:        &reversal.OriginalSourceId,
		Memo:            textOrNil(reversal.Reason),
		ReversesEntryId: &originalEntry.Id,
		CreatedBy:       reversal.UserId,
		Lines:           swapSides(originalLines),
	}
	return ledger.post(ctx, querier, books, reversedEntry)
}

func (ledger *Ledger) postPartyOpening(ctx context.Context, querier database.Querier, books Books, opening partyOpening, happenedAt time.Time) (*PostedEntry, error) {
	difference := opening.OpeningBalance - opening.PostedBalance
	controlKey := KeyReceivable
	if opening.PartyType == PartySupplier {
		controlKey = KeyPayable
		difference = -difference
	}
	openingLines := []keyedLine{
		{Key: controlKey, Debit: difference},
		{Key: KeyOwnerCapital, Credit: difference},
	}
	if difference < 0 {
		openingLines = []keyedLine{
			{Key: KeyOwnerCapital, Debit: -difference},
			{Key: controlKey, Credit: -difference},
		}
	}
	partyId := opening.PartyId
	memo := openingPartyMemo
	header := Entry{
		SourceType: SourceOpening,
		Memo:       &memo,
		PartyType:  &opening.PartyType,
		PartyId:    &partyId,
	}
	return ledger.postEventWithBooks(ctx, querier, books, happenedAt, header, openingLines)
}

func swapSides(originalLines []Line) []Line {
	swappedLines := make([]Line, 0, len(originalLines))
	for _, originalLine := range originalLines {
		swappedLines = append(swappedLines, Line{
			AccountId: originalLine.AccountId,
			Debit:     originalLine.Credit,
			Credit:    originalLine.Debit,
			ShopId:    originalLine.ShopId,
		})
	}
	return swappedLines
}

func splitVat(books Books, netCost int64, vat int64) (int64, int64) {
	if books.VatRegistered {
		return netCost, vat
	}
	return netCost + vat, 0
}
