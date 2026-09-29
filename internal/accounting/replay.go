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

func (ledger *Ledger) PostSaleById(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, &saleId, nil, nil)
}

func (ledger *Ledger) PostStockMovementById(ctx context.Context, querier database.Querier, companyId uuid.UUID, movementId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, nil, &movementId, nil)
}

func (ledger *Ledger) PostTransferById(ctx context.Context, querier database.Querier, companyId uuid.UUID, transferId uuid.UUID) error {
	return ledger.postPendingById(ctx, querier, companyId, nil, nil, &transferId)
}

func (ledger *Ledger) postPendingById(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId *uuid.UUID, movementId *uuid.UUID, transferId *uuid.UUID) error {
	books, booksError := ledger.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return booksError
	}
	if !books.IsPosting() {
		return nil
	}

	pendingEvents := []pendingEvent{}
	loadError := error(nil)
	switch {
	case saleId != nil:
		pendingEvents, loadError = ledger.pendingSales(ctx, querier, books, saleId)
	case movementId != nil:
		pendingEvents, loadError = ledger.pendingMovements(ctx, querier, books, movementId)
	case transferId != nil:
		pendingEvents, loadError = ledger.pendingTransfers(ctx, querier, books, transferId)
	}
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

func (ledger *Ledger) pendingEvents(ctx context.Context, querier database.Querier, books Books) ([]pendingEvent, error) {
	saleEvents, salesError := ledger.pendingSales(ctx, querier, books, nil)
	if salesError != nil {
		return nil, salesError
	}
	movementEvents, movementsError := ledger.pendingMovements(ctx, querier, books, nil)
	if movementsError != nil {
		return nil, movementsError
	}
	transferEvents, transfersError := ledger.pendingTransfers(ctx, querier, books, nil)
	if transfersError != nil {
		return nil, transfersError
	}

	allEvents := append(append(saleEvents, movementEvents...), transferEvents...)
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

func (ledger *Ledger) pendingSales(ctx context.Context, querier database.Querier, books Books, onlySaleId *uuid.UUID) ([]pendingEvent, error) {
	salePostings, loadError := ledger.repository.UnpostedSales(ctx, querier, books.CompanyId, books.StartedAt, onlySaleId)
	if loadError != nil {
		return nil, loadError
	}
	events := make([]pendingEvent, 0, len(salePostings))
	for _, salePosting := range salePostings {
		posting := salePosting
		events = append(events, pendingEvent{
			happenedAt: posting.SoldAt,
			hasValue:   posting.Total > 0 || posting.CostTotal > 0,
			post: func() (*PostedEntry, error) {
				return ledger.postSale(ctx, querier, books, posting)
			},
		})
	}
	return events, nil
}

func (ledger *Ledger) pendingMovements(ctx context.Context, querier database.Querier, books Books, onlyMovementId *uuid.UUID) ([]pendingEvent, error) {
	movementPostings, loadError := ledger.repository.UnpostedMovements(ctx, querier, books.CompanyId, books.StartedAt, onlyMovementId)
	if loadError != nil {
		return nil, loadError
	}
	events := make([]pendingEvent, 0, len(movementPostings))
	for _, movementPosting := range movementPostings {
		posting := movementPosting
		events = append(events, pendingEvent{
			happenedAt: posting.MovedAt,
			hasValue:   posting.UnitCost > 0,
			post: func() (*PostedEntry, error) {
				return ledger.postStockMovement(ctx, querier, books, posting)
			},
		})
	}
	return events, nil
}

func (ledger *Ledger) pendingTransfers(ctx context.Context, querier database.Querier, books Books, onlyTransferId *uuid.UUID) ([]pendingEvent, error) {
	transferPostings, loadError := ledger.repository.UnpostedTransfers(ctx, querier, books.CompanyId, books.StartedAt, onlyTransferId)
	if loadError != nil {
		return nil, loadError
	}
	events := make([]pendingEvent, 0, len(transferPostings))
	for _, transferPosting := range transferPostings {
		posting := transferPosting
		events = append(events, pendingEvent{
			happenedAt: posting.SentAt,
			hasValue:   posting.ValueAtCost > 0,
			post: func() (*PostedEntry, error) {
				return ledger.postTransfer(ctx, querier, books, posting)
			},
		})
	}
	return events, nil
}
