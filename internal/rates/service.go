package rates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
)

const (
	providerBaseCurrency = "USD"
	providerName         = "ExchangeRate-API"
	providerSiteUrl      = "https://www.exchangerate-api.com"
	providerTimeout      = 5 * time.Second
	freshFor             = 6 * time.Hour
	providerResponseMax  = 256 * 1024
)

var (
	providerUrl       = "https://open.er-api.com/v6/latest/" + providerBaseCurrency
	providerTransport = http.DefaultTransport
	retryAfterFailure = 5 * time.Minute
	shownCurrencies   = []string{"USD", "EUR", "GBP", "KES", "UGX", "RWF", "CNY", "AED", "INR", "ZAR"}
)

type RateView struct {
	Code  string  `json:"code"`
	Value float64 `json:"value"`
}

type RatesView struct {
	Available         bool       `json:"available"`
	BaseCurrency      string     `json:"base_currency"`
	Rates             []RateView `json:"rates"`
	ProviderUpdatedAt *time.Time `json:"provider_updated_at"`
	FetchedAt         *time.Time `json:"fetched_at"`
	IsStale           bool       `json:"is_stale"`
	Problem           string     `json:"problem"`
	SourceName        string     `json:"source_name"`
	SourceUrl         string     `json:"source_url"`
}

type providerAnswer struct {
	Result             string             `json:"result"`
	BaseCode           string             `json:"base_code"`
	TimeLastUpdateUnix int64              `json:"time_last_update_unix"`
	Rates              map[string]float64 `json:"rates"`
}

type Service struct {
	openDatabase  *database.Database
	repository    *Repository
	httpClient    *http.Client
	refreshMutex  sync.Mutex
	isRefreshing  bool
	lastFailureAt time.Time
}

func NewService(openDatabase *database.Database, repository *Repository) *Service {
	return &Service{
		openDatabase: openDatabase,
		repository:   repository,
		httpClient:   &http.Client{Timeout: providerTimeout, Transport: providerTransport},
	}
}

func (service *Service) Latest(ctx context.Context, companyCurrency string) (RatesView, error) {
	storedSnapshot, findError := service.repository.FindSnapshot(ctx, service.openDatabase.Writer, providerBaseCurrency)
	if findError != nil {
		return RatesView{}, findError
	}

	hasSnapshot := storedSnapshot != nil
	isFresh := hasSnapshot && time.Since(storedSnapshot.FetchedAt) < freshFor
	if !hasSnapshot {
		refreshedSnapshot, refreshError := service.refresh(ctx)
		if refreshError != nil {
			return unavailable(companyCurrency, "Exchange rates need an internet connection the first time. They will show once this computer is online."), nil
		}
		storedSnapshot = refreshedSnapshot
		isFresh = true
	}
	if !isFresh {
		go service.refreshInBackground()
	}

	return buildView(companyCurrency, *storedSnapshot, !isFresh), nil
}

func (service *Service) refreshInBackground() {
	backgroundContext, cancel := context.WithTimeout(context.Background(), providerTimeout+time.Second)
	defer cancel()
	_, refreshError := service.refresh(backgroundContext)
	if refreshError != nil {
		slog.Warn("exchange rates stay stale", "error", refreshError)
	}
}

func (service *Service) refresh(ctx context.Context) (*Snapshot, error) {
	service.refreshMutex.Lock()
	isBackingOff := time.Since(service.lastFailureAt) < retryAfterFailure
	if service.isRefreshing || isBackingOff {
		service.refreshMutex.Unlock()
		return nil, errors.New("exchange rates are already refreshing or recently failed")
	}
	service.isRefreshing = true
	service.refreshMutex.Unlock()

	fetchedSnapshot, fetchError := service.fetch(ctx)

	service.refreshMutex.Lock()
	service.isRefreshing = false
	if fetchError != nil {
		service.lastFailureAt = time.Now()
	}
	service.refreshMutex.Unlock()
	if fetchError != nil {
		return nil, fetchError
	}

	saveError := service.repository.SaveSnapshot(ctx, service.openDatabase.Writer, *fetchedSnapshot)
	if saveError != nil {
		return nil, saveError
	}
	return fetchedSnapshot, nil
}

func (service *Service) fetch(ctx context.Context) (*Snapshot, error) {
	providerRequest, requestError := http.NewRequestWithContext(ctx, http.MethodGet, providerUrl, nil)
	if requestError != nil {
		return nil, fmt.Errorf("failed to build the exchange rate request: %w", requestError)
	}
	providerRequest.Header.Set("Accept", "application/json")

	slog.Info("calling exchange rate provider", "url", providerUrl)
	startedAt := time.Now()
	providerResponse, sendError := service.httpClient.Do(providerRequest)
	if sendError != nil {
		return nil, fmt.Errorf("exchange rate provider unreachable: %w", sendError)
	}
	defer providerResponse.Body.Close()
	slog.Info("exchange rate provider responded", "status", providerResponse.StatusCode, "durationMs", time.Since(startedAt).Milliseconds())

	if providerResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange rate provider answered %d", providerResponse.StatusCode)
	}
	responseBytes, readError := io.ReadAll(io.LimitReader(providerResponse.Body, providerResponseMax))
	if readError != nil {
		return nil, fmt.Errorf("failed to read exchange rates: %w", readError)
	}

	answer := providerAnswer{}
	unmarshalError := json.Unmarshal(responseBytes, &answer)
	if unmarshalError != nil {
		return nil, fmt.Errorf("exchange rates were not valid JSON: %w", unmarshalError)
	}
	baseRate := answer.Rates[providerBaseCurrency]
	isUsable := answer.Result == "success" && answer.BaseCode == providerBaseCurrency && baseRate == 1 && len(answer.Rates) > 1
	if !isUsable {
		return nil, fmt.Errorf("exchange rate provider answered %q for base %q", answer.Result, answer.BaseCode)
	}

	positiveRates := map[string]float64{}
	for currencyCode, rate := range answer.Rates {
		if rate > 0 {
			positiveRates[currencyCode] = rate
		}
	}
	ratesBytes, marshalError := json.Marshal(positiveRates)
	if marshalError != nil {
		return nil, fmt.Errorf("failed to store exchange rates: %w", marshalError)
	}

	fetchedSnapshot := Snapshot{
		BaseCurrency: providerBaseCurrency,
		RatesJson:    string(ratesBytes),
		FetchedAt:    time.Now().UTC(),
	}
	if answer.TimeLastUpdateUnix > 0 {
		providerUpdatedAt := time.Unix(answer.TimeLastUpdateUnix, 0).UTC()
		fetchedSnapshot.ProviderUpdatedAt = &providerUpdatedAt
	}
	return &fetchedSnapshot, nil
}

func buildView(companyCurrency string, storedSnapshot Snapshot, isStale bool) RatesView {
	providerRates := map[string]float64{}
	unmarshalError := json.Unmarshal([]byte(storedSnapshot.RatesJson), &providerRates)
	if unmarshalError != nil {
		return unavailable(companyCurrency, "The saved exchange rates could not be read. They will refresh when online.")
	}

	companyRate, hasCompanyRate := providerRates[companyCurrency]
	if !hasCompanyRate {
		return unavailable(companyCurrency, fmt.Sprintf("No exchange rates are published for %s.", companyCurrency))
	}

	rateViews := []RateView{}
	for _, shownCurrency := range shownCurrencies {
		shownRate, hasShownRate := providerRates[shownCurrency]
		if !hasShownRate || shownCurrency == companyCurrency {
			continue
		}
		rateView := RateView{
			Code:  shownCurrency,
			Value: companyRate / shownRate,
		}
		rateViews = append(rateViews, rateView)
	}

	fetchedAt := storedSnapshot.FetchedAt
	ratesView := RatesView{
		Available:         true,
		BaseCurrency:      companyCurrency,
		Rates:             rateViews,
		ProviderUpdatedAt: storedSnapshot.ProviderUpdatedAt,
		FetchedAt:         &fetchedAt,
		IsStale:           isStale,
		SourceName:        providerName,
		SourceUrl:         providerSiteUrl,
	}
	return ratesView
}

func unavailable(companyCurrency string, problem string) RatesView {
	return RatesView{
		Available:    false,
		BaseCurrency: companyCurrency,
		Rates:        []RateView{},
		Problem:      problem,
		SourceName:   providerName,
		SourceUrl:    providerSiteUrl,
	}
}
