package accounting

import (
	"context"
	"sort"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type pendingEvent struct {
	happenedAt time.Time
	hasValue   bool
	post       func() (*PostedEntry, error)
}

type pendingLoader func(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error)

func (ledger *Ledger) PostSaleById(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, saleId, ledger.pendingSales)
}

func (ledger *Ledger) PostStockMovementById(ctx context.Context, querier database.Querier, companyId uuid.UUID, movementId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, movementId, ledger.pendingMovements)
}

func (ledger *Ledger) PostTransferById(ctx context.Context, querier database.Querier, companyId uuid.UUID, transferId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, transferId, ledger.pendingTransfers)
}

func (ledger *Ledger) PostPurchaseById(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, purchaseId, ledger.pendingPurchases)
}

func (ledger *Ledger) PostPurchaseCancelById(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, purchaseId, ledger.pendingPurchaseCancels)
}

func (ledger *Ledger) PostSupplierPaymentById(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, paymentId, ledger.pendingSupplierPayments)
}

func (ledger *Ledger) PostSupplierPaymentVoidById(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, paymentId, ledger.pendingSupplierPaymentVoids)
}

func (ledger *Ledger) PostSupplierReturnById(ctx context.Context, querier database.Querier, companyId uuid.UUID, returnId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, returnId, ledger.pendingSupplierReturns)
}

func (ledger *Ledger) PostCustomerPaymentById(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, paymentId, ledger.pendingCustomerPayments)
}

func (ledger *Ledger) PostCustomerPaymentVoidById(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, paymentId, ledger.pendingCustomerPaymentVoids)
}

func (ledger *Ledger) PostOrderMoneyById(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderPaymentId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, orderPaymentId, ledger.pendingOrderMoney)
}

func (ledger *Ledger) SyncSupplierOpening(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, supplierId, ledger.pendingSupplierOpenings)
}

func (ledger *Ledger) SyncCustomerOpening(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, customerId, ledger.pendingCustomerOpenings)
}

func (ledger *Ledger) postPendingById(ctx context.Context, querier database.Querier, companyId uuid.UUID, documentId uuid.UUID, loadPending pendingLoader) error {
	books, booksError := ledger.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return booksError
	}
	if !books.IsPosting() {
		return nil
	}
	pendingEvents, loadError := loadPending(ctx, querier, books, &documentId)
	if loadError != nil {
		return loadError
	}
	for _, event := range pendingEvents {
		_, postError := event.post()
		if postError != nil {
			return postError
		}
	}
	return nil
}

func (ledger *Ledger) pendingLoaders() []pendingLoader {
	return []pendingLoader{
		ledger.pendingSupplierOpenings,
		ledger.pendingCustomerOpenings,
		ledger.pendingSales,
		ledger.pendingMovements,
		ledger.pendingTransfers,
		ledger.pendingPurchases,
		ledger.pendingPurchaseCancels,
		ledger.pendingSupplierPayments,
		ledger.pendingSupplierPaymentVoids,
		ledger.pendingSupplierReturns,
		ledger.pendingCustomerPayments,
		ledger.pendingCustomerPaymentVoids,
		ledger.pendingOrderMoney,
	}
}

func (ledger *Ledger) pendingEvents(ctx context.Context, querier database.Querier, books Books) ([]pendingEvent, error) {
	allEvents := []pendingEvent{}
	for _, loadPending := range ledger.pendingLoaders() {
		loadedEvents, loadError := loadPending(ctx, querier, books, nil)
		if loadError != nil {
			return nil, loadError
		}
		allEvents = append(allEvents, loadedEvents...)
	}
	sort.SliceStable(allEvents, func(left int, right int) bool {
		return allEvents[left].happenedAt.Before(allEvents[right].happenedAt)
	})
	return allEvents, nil
}

func (ledger *Ledger) CountUnposted(ctx context.Context, querier database.Querier, books Books) (int, error) {
	if !books.IsPosting() {
		return 0, nil
	}
	allEvents, loadError := ledger.pendingEvents(ctx, querier, books)
	if loadError != nil {
		return 0, loadError
	}
	unpostedCount := 0
	for _, event := range allEvents {
		if event.hasValue {
			unpostedCount++
		}
	}
	return unpostedCount, nil
}

func (ledger *Ledger) PostUnposted(ctx context.Context, querier database.Querier, books Books) (int, error) {
	allEvents, loadError := ledger.pendingEvents(ctx, querier, books)
	if loadError != nil {
		return 0, loadError
	}
	postedCount := 0
	for _, event := range allEvents {
		postedEntry, postError := event.post()
		if postError != nil {
			return postedCount, postError
		}
		if postedEntry != nil {
			postedCount++
		}
	}
	return postedCount, nil
}

func eventsOf[Posting any](postings []Posting, loadError error, describe func(posting Posting) pendingEvent) ([]pendingEvent, error) {
	if loadError != nil {
		return nil, loadError
	}
	events := make([]pendingEvent, 0, len(postings))
	for _, posting := range postings {
		events = append(events, describe(posting))
	}
	return events, nil
}

func (ledger *Ledger) pendingSales(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	salePostings, loadError := ledger.repository.UnpostedSales(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(salePostings, loadError, func(posting SalePosting) pendingEvent {
		return pendingEvent{posting.SoldAt, posting.Total > 0 || posting.CostTotal > 0, func() (*PostedEntry, error) {
			return ledger.postSale(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingMovements(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	movementPostings, loadError := ledger.repository.UnpostedMovements(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(movementPostings, loadError, func(posting StockMovementPosting) pendingEvent {
		return pendingEvent{posting.MovedAt, posting.UnitCost > 0, func() (*PostedEntry, error) {
			return ledger.postStockMovement(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingTransfers(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	transferPostings, loadError := ledger.repository.UnpostedTransfers(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(transferPostings, loadError, func(posting TransferPosting) pendingEvent {
		return pendingEvent{posting.SentAt, posting.ValueAtCost > 0, func() (*PostedEntry, error) {
			return ledger.postTransfer(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingPurchases(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	purchasePostings, loadError := ledger.repository.UnpostedPurchases(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(purchasePostings, loadError, func(posting PurchasePosting) pendingEvent {
		return pendingEvent{posting.ReceivedAt, posting.OwedToSupplier > 0, func() (*PostedEntry, error) {
			return ledger.postPurchase(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingSupplierReturns(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	returnPostings, loadError := ledger.repository.UnpostedSupplierReturns(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(returnPostings, loadError, func(posting SupplierReturnPosting) pendingEvent {
		return pendingEvent{posting.ReturnedAt, posting.NetCost > 0, func() (*PostedEntry, error) {
			return ledger.postSupplierReturn(ctx, querier, books, posting)
		}}
	})
}

func (ledger *Ledger) pendingSupplierPayments(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	paymentPostings, loadError := ledger.repository.UnpostedSupplierPayments(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(paymentPostings, loadError, func(posting PaymentPosting) pendingEvent {
		return pendingEvent{posting.PaidAt, true, func() (*PostedEntry, error) {
			return ledger.postPaymentWithBooks(ctx, querier, books, posting, SourceSupplierPayment)
		}}
	})
}

func (ledger *Ledger) pendingCustomerPayments(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	paymentPostings, loadError := ledger.repository.UnpostedCustomerPayments(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(paymentPostings, loadError, func(posting PaymentPosting) pendingEvent {
		return pendingEvent{posting.PaidAt, true, func() (*PostedEntry, error) {
			return ledger.postPaymentWithBooks(ctx, querier, books, posting, SourceCustomerPayment)
		}}
	})
}

func (ledger *Ledger) pendingOrderMoney(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	orderMoneyPostings, loadError := ledger.repository.UnpostedOrderMoney(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return eventsOf(orderMoneyPostings, loadError, func(orderMoney orderMoneyPosting) pendingEvent {
		sourceType := SourceOrderDeposit
		if orderMoney.Kind == "refund" {
			sourceType = SourceOrderRefund
		}
		return pendingEvent{orderMoney.Payment.PaidAt, true, func() (*PostedEntry, error) {
			return ledger.postPaymentWithBooks(ctx, querier, books, orderMoney.Payment, sourceType)
		}}
	})
}

func (ledger *Ledger) reversalEvents(ctx context.Context, querier database.Querier, books Books, reversals []ReversalPosting, loadError error) ([]pendingEvent, error) {
	return eventsOf(reversals, loadError, func(reversal ReversalPosting) pendingEvent {
		return pendingEvent{reversal.ReversedAt, true, func() (*PostedEntry, error) {
			return ledger.reverseSourceWithBooks(ctx, querier, books, reversal)
		}}
	})
}

func (ledger *Ledger) pendingPurchaseCancels(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	reversals, loadError := ledger.repository.UnpostedPurchaseCancels(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return ledger.reversalEvents(ctx, querier, books, reversals, loadError)
}

func (ledger *Ledger) pendingSupplierPaymentVoids(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	reversals, loadError := ledger.repository.UnpostedSupplierPaymentVoids(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return ledger.reversalEvents(ctx, querier, books, reversals, loadError)
}

func (ledger *Ledger) pendingCustomerPaymentVoids(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	reversals, loadError := ledger.repository.UnpostedCustomerPaymentVoids(ctx, querier, books.CompanyId, books.StartedAt, onlyId)
	return ledger.reversalEvents(ctx, querier, books, reversals, loadError)
}

func (ledger *Ledger) pendingSupplierOpenings(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	openings, loadError := ledger.repository.UnpostedPartyOpenings(ctx, querier, books.CompanyId, PartySupplier, onlyId)
	return ledger.openingEvents(ctx, querier, books, openings, loadError)
}

func (ledger *Ledger) pendingCustomerOpenings(ctx context.Context, querier database.Querier, books Books, onlyId *uuid.UUID) ([]pendingEvent, error) {
	openings, loadError := ledger.repository.UnpostedPartyOpenings(ctx, querier, books.CompanyId, PartyCustomer, onlyId)
	return ledger.openingEvents(ctx, querier, books, openings, loadError)
}

func (ledger *Ledger) openingEvents(ctx context.Context, querier database.Querier, books Books, openings []partyOpening, loadError error) ([]pendingEvent, error) {
	return eventsOf(openings, loadError, func(opening partyOpening) pendingEvent {
		happenedAt := opening.CreatedAt
		if happenedAt.Before(books.StartedAt) {
			happenedAt = books.StartedAt
		}
		return pendingEvent{happenedAt, true, func() (*PostedEntry, error) {
			return ledger.postPartyOpening(ctx, querier, books, opening, happenedAt)
		}}
	})
}
