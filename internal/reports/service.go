package reports

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/google/uuid"
)

const (
	maximumRangeDays      = 366
	defaultRangeDays      = 30
	dateLayout            = "2006-01-02"
	allShops              = "all"
	defaultProductLimit   = 20
	maximumProductLimit   = 100
	deadStockLimit        = 50
	dashboardTopProducts  = 5
	dashboardRecentSales  = 5
	dashboardTrendDays    = 14
	dashboardProductsDays = 30
)

var (
	ErrNoActiveShop    = errors.New("choose a shop first")
	ErrShopNotAssigned = errors.New("you can only see reports for shops you work in")
	ErrShopNotFound    = errors.New("shop not found")
	ErrInvalidRange    = errors.New("use dates like 2026-09-29, with the start on or before the end")
	ErrRangeTooLong    = errors.New("choose a range of at most 366 days")
	ErrInvalidSort     = errors.New("sort by revenue, quantity or profit")
	ErrMissingSettings = errors.New("company settings are missing")
)

var productOrderColumns = map[string]string{
	"revenue":  "SUM(i.line_total)",
	"quantity": "SUM(i.quantity)",
	"profit":   "SUM(i.line_total * 10000 / (10000 + s.tax_rate_basis_points)) - SUM(i.unit_cost * i.quantity)",
}

type RangeRequest struct {
	FromDate string
	ToDate   string
	Shop     string
}

type Service struct {
	repository         *Repository
	settingsRepository *settings.Repository
	objectStore        storage.Store
}

func NewService(repository *Repository, settingsRepository *settings.Repository, objectStore storage.Store) *Service {
	return &Service{
		repository:         repository,
		settingsRepository: settingsRepository,
		objectStore:        objectStore,
	}
}

func (service *Service) Summary(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest) (SummaryView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return SummaryView{}, scopeError
	}
	return service.summaryFor(ctx, querier, scope)
}

func (service *Service) Daily(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest) ([]DayView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return nil, scopeError
	}
	return service.repository.Daily(ctx, querier, scope, dayBucketsFor(scope))
}

func (service *Service) Products(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest, sortBy string, limit int) ([]ProductRowView, error) {
	orderColumn, isKnownSort := productOrderColumns[sortBy]
	if sortBy == "" {
		orderColumn, isKnownSort = productOrderColumns["revenue"], true
	}
	if !isKnownSort {
		return nil, ErrInvalidSort
	}
	if limit < 1 {
		limit = defaultProductLimit
	}
	if limit > maximumProductLimit {
		limit = maximumProductLimit
	}

	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return nil, scopeError
	}
	return service.repository.Products(ctx, querier, scope, orderColumn, limit)
}

func (service *Service) Cashiers(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest) ([]CashierRowView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return nil, scopeError
	}
	return service.repository.Cashiers(ctx, querier, scope)
}

func (service *Service) Shops(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest) ([]ShopRowView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return nil, scopeError
	}
	return service.repository.Shops(ctx, querier, scope)
}

func (service *Service) Inventory(ctx context.Context, querier database.Querier, principal *identity.Principal, shop string) (InventoryView, error) {
	scope, scopeError := service.scope(ctx, querier, principal, RangeRequest{Shop: shop})
	if scopeError != nil {
		return InventoryView{}, scopeError
	}
	companySettings, settingsError := service.settingsRepository.FindSettings(ctx, querier, principal.CompanyId)
	if settingsError != nil {
		return InventoryView{}, settingsError
	}
	if companySettings == nil {
		return InventoryView{}, ErrMissingSettings
	}

	stockTotals, stockError := service.repository.StockTotals(ctx, querier, scope)
	if stockError != nil {
		return InventoryView{}, stockError
	}
	soldBefore := time.Now().UTC().AddDate(0, 0, -companySettings.DeadStockDays)
	deadStock, deadError := service.repository.DeadStock(ctx, querier, scope, soldBefore, deadStockLimit)
	if deadError != nil {
		return InventoryView{}, deadError
	}

	inventoryView := InventoryView{
		Stock:         stockTotals,
		DeadStockDays: companySettings.DeadStockDays,
		DeadStock:     deadStock,
	}
	return inventoryView, nil
}

func (service *Service) Dashboard(ctx context.Context, querier database.Querier, principal *identity.Principal, shop string) (DashboardView, error) {
	todayScope, scopeError := service.scope(ctx, querier, principal, RangeRequest{Shop: shop})
	if scopeError != nil {
		return DashboardView{}, scopeError
	}
	today := todayScope.LastDay
	withDays := func(firstDay time.Time, lastDay time.Time) Scope {
		return rangeScope(todayScope, firstDay, lastDay)
	}

	todaySummary, todayError := service.summaryFor(ctx, querier, withDays(today, today))
	if todayError != nil {
		return DashboardView{}, todayError
	}
	yesterday := today.AddDate(0, 0, -1)
	yesterdaySummary, yesterdayError := service.summaryFor(ctx, querier, withDays(yesterday, yesterday))
	if yesterdayError != nil {
		return DashboardView{}, yesterdayError
	}
	firstOfMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, todayScope.Location)
	monthSummary, monthError := service.summaryFor(ctx, querier, withDays(firstOfMonth, today))
	if monthError != nil {
		return DashboardView{}, monthError
	}

	trendScope := withDays(today.AddDate(0, 0, -(dashboardTrendDays-1)), today)
	trendDays, trendError := service.repository.Daily(ctx, querier, trendScope, dayBucketsFor(trendScope))
	if trendError != nil {
		return DashboardView{}, trendError
	}
	productsScope := withDays(today.AddDate(0, 0, -(dashboardProductsDays-1)), today)
	topProducts, productsError := service.repository.Products(ctx, querier, productsScope, productOrderColumns["revenue"], dashboardTopProducts)
	if productsError != nil {
		return DashboardView{}, productsError
	}
	stockTotals, stockError := service.repository.StockTotals(ctx, querier, todayScope)
	if stockError != nil {
		return DashboardView{}, stockError
	}
	recentSales, recentError := service.repository.RecentSales(ctx, querier, todayScope, dashboardRecentSales)
	if recentError != nil {
		return DashboardView{}, recentError
	}

	dashboardView := DashboardView{
		Today:        todaySummary,
		Yesterday:    yesterdaySummary,
		MonthToDate:  monthSummary,
		LastTwoWeeks: trendDays,
		TopProducts:  topProducts,
		Stock:        stockTotals,
		RecentSales:  recentSales,
	}
	return dashboardView, nil
}

func (service *Service) summaryFor(ctx context.Context, querier database.Querier, scope Scope) (SummaryView, error) {
	summary, totalsError := service.repository.SaleTotals(ctx, querier, scope)
	if totalsError != nil {
		return SummaryView{}, totalsError
	}
	summary.FromDate = scope.FromDate
	summary.ToDate = scope.ToDate
	summary.NetSales = summary.Total - summary.TaxTotal
	summary.GrossProfit = summary.NetSales - summary.CostTotal
	summary.AverageSale = averageOf(summary.Total, summary.SaleCount)
	if summary.NetSales > 0 {
		summary.MarginBasisPoints = (summary.GrossProfit*10000 + summary.NetSales/2) / summary.NetSales
	}
	return summary, nil
}

func (service *Service) scope(ctx context.Context, querier database.Querier, principal *identity.Principal, rangeRequest RangeRequest) (Scope, error) {
	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, querier, principal.CompanyId)
	if profileError != nil {
		return Scope{}, profileError
	}
	if companyProfile == nil {
		return Scope{}, ErrMissingSettings
	}
	companyLocation, locationError := time.LoadLocation(companyProfile.Timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}

	nowInCompany := time.Now().In(companyLocation)
	today := time.Date(nowInCompany.Year(), nowInCompany.Month(), nowInCompany.Day(), 0, 0, 0, 0, companyLocation)
	lastDay := today
	if rangeRequest.ToDate != "" {
		parsedDay, parseError := time.ParseInLocation(dateLayout, rangeRequest.ToDate, companyLocation)
		if parseError != nil {
			return Scope{}, ErrInvalidRange
		}
		lastDay = parsedDay
	}
	firstDay := lastDay.AddDate(0, 0, -(defaultRangeDays - 1))
	if rangeRequest.FromDate != "" {
		parsedDay, parseError := time.ParseInLocation(dateLayout, rangeRequest.FromDate, companyLocation)
		if parseError != nil {
			return Scope{}, ErrInvalidRange
		}
		firstDay = parsedDay
	}
	if firstDay.After(lastDay) {
		return Scope{}, ErrInvalidRange
	}
	if firstDay.AddDate(0, 0, maximumRangeDays).Before(lastDay.AddDate(0, 0, 1)) {
		return Scope{}, ErrRangeTooLong
	}

	scope := Scope{
		CompanyId: principal.CompanyId,
		Location:  companyLocation,
	}
	shopError := service.resolveShops(ctx, querier, principal, rangeRequest.Shop, &scope)
	if shopError != nil {
		return Scope{}, shopError
	}
	return rangeScope(scope, firstDay, lastDay), nil
}

func (service *Service) resolveShops(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string, scope *Scope) error {
	if requestedShop == "" {
		if principal.ShopId == nil {
			return ErrNoActiveShop
		}
		scope.ShopIds = []uuid.UUID{*principal.ShopId}
		return nil
	}

	if requestedShop == allShops {
		if principal.IsOwner {
			scope.AllShops = true
			return nil
		}
		assignedShopIds, assignedError := service.repository.AssignedShopIds(ctx, querier, principal.CompanyId, principal.UserId)
		if assignedError != nil {
			return assignedError
		}
		if len(assignedShopIds) == 0 {
			return ErrShopNotAssigned
		}
		scope.ShopIds = assignedShopIds
		return nil
	}

	shopId, parseError := uuid.Parse(requestedShop)
	if parseError != nil {
		return ErrShopNotFound
	}
	shopExists, existsError := service.repository.ShopExists(ctx, querier, principal.CompanyId, shopId)
	if existsError != nil {
		return existsError
	}
	if !shopExists {
		return ErrShopNotFound
	}
	if !principal.IsOwner {
		assignedShopIds, assignedError := service.repository.AssignedShopIds(ctx, querier, principal.CompanyId, principal.UserId)
		if assignedError != nil {
			return assignedError
		}
		isAssigned := false
		for _, assignedShopId := range assignedShopIds {
			isAssigned = isAssigned || assignedShopId == shopId
		}
		if !isAssigned {
			return ErrShopNotAssigned
		}
	}
	scope.ShopIds = []uuid.UUID{shopId}
	return nil
}

func rangeScope(baseScope Scope, firstDay time.Time, lastDay time.Time) Scope {
	rangedScope := baseScope
	firstMidnight := time.Date(firstDay.Year(), firstDay.Month(), firstDay.Day(), 0, 0, 0, 0, baseScope.Location)
	afterLastMidnight := time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day()+1, 0, 0, 0, 0, baseScope.Location)
	rangedScope.FirstDay = firstMidnight
	rangedScope.LastDay = time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 0, 0, 0, 0, baseScope.Location)
	rangedScope.From = firstMidnight.UTC()
	rangedScope.To = afterLastMidnight.UTC()
	rangedScope.FromDate = firstMidnight.Format(dateLayout)
	rangedScope.ToDate = lastDay.Format(dateLayout)
	return rangedScope
}

func dayBucketsFor(scope Scope) []dayBucket {
	dayBuckets := []dayBucket{}
	for day := scope.FirstDay; !day.After(scope.LastDay); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, scope.Location) {
		nextDay := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, scope.Location)
		bucket := dayBucket{
			label:    day.Format(dateLayout),
			startsAt: day.UTC(),
			endsAt:   nextDay.UTC(),
		}
		dayBuckets = append(dayBuckets, bucket)
	}
	return dayBuckets
}
