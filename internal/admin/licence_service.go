package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/google/uuid"
)

const cloudDevicePrefix = "cloud-"

func (service *Service) ConnectWapangaji(ctx context.Context, querier database.Querier, staff Staff, sessionToken string, request WapangajiConnectRequest) (WapangajiStatusView, error) {
	tokens, signInError := service.wapangaji.signIn(ctx, strings.TrimSpace(request.Phone), request.Password)
	if signInError != nil {
		return WapangajiStatusView{}, signInError
	}
	saveError := service.repository.SaveWapangajiTokens(ctx, querier, security.HashSessionToken(sessionToken), tokens.Access, tokens.Refresh, tokens.Name)
	if saveError != nil {
		return WapangajiStatusView{}, saveError
	}
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "connected_wapangaji", nil, tokens.Name, time.Now().UTC())
	if auditError != nil {
		return WapangajiStatusView{}, auditError
	}
	return WapangajiStatusView{Connected: true, Name: tokens.Name}, nil
}

func (service *Service) DisconnectWapangaji(ctx context.Context, querier database.Querier, sessionToken string) error {
	return service.repository.ClearWapangajiTokens(ctx, querier, security.HashSessionToken(sessionToken))
}

func (service *Service) WapangajiStatus(ctx context.Context, querier database.Querier, sessionToken string) (WapangajiStatusView, error) {
	tokens, findError := service.repository.FindWapangajiTokens(ctx, querier, security.HashSessionToken(sessionToken))
	if findError != nil {
		return WapangajiStatusView{}, findError
	}
	if tokens == nil {
		return WapangajiStatusView{}, nil
	}
	return WapangajiStatusView{Connected: true, Name: tokens.Name}, nil
}

func (service *Service) withWapangaji(ctx context.Context, querier database.Querier, sessionToken string, wapangajiCall func(accessToken string) error) error {
	tokenHash := security.HashSessionToken(sessionToken)
	tokens, findError := service.repository.FindWapangajiTokens(ctx, querier, tokenHash)
	if findError != nil {
		return findError
	}
	if tokens == nil {
		return ErrWapangajiNotConnected
	}

	firstCallError := wapangajiCall(tokens.Access)
	wapangajiError := &WapangajiError{}
	isExpired := errors.As(firstCallError, &wapangajiError) && wapangajiError.StatusCode == http.StatusUnauthorized
	if !isExpired {
		return firstCallError
	}

	freshAccess, freshRefresh, refreshError := service.wapangaji.refresh(ctx, tokens.Refresh)
	if refreshError != nil {
		clearError := service.repository.ClearWapangajiTokens(ctx, querier, tokenHash)
		if clearError != nil {
			return clearError
		}
		return ErrWapangajiNotConnected
	}
	saveError := service.repository.SaveWapangajiTokens(ctx, querier, tokenHash, freshAccess, freshRefresh, tokens.Name)
	if saveError != nil {
		return saveError
	}
	return wapangajiCall(freshAccess)
}

func (service *Service) LookupLicence(ctx context.Context, querier database.Querier, sessionToken string, request LicenceLookupRequest) (LicenceLookupView, error) {
	hardwareId := strings.TrimSpace(request.HardwareId)
	lookupReply := struct {
		Found bool        `json:"found"`
		Data  LicenceView `json:"data"`
	}{}
	lookupError := service.withWapangaji(ctx, querier, sessionToken, func(accessToken string) error {
		return service.wapangaji.call(ctx, http.MethodGet, "/payments/balce/admin/lookup/"+licenceQuery(hardwareId, strings.TrimSpace(request.Phone)), accessToken, nil, &lookupReply)
	})
	if lookupError != nil {
		return LicenceLookupView{}, lookupError
	}

	lookupView := LicenceLookupView{Found: lookupReply.Found}
	if lookupReply.Found {
		lookupView.Licence = &lookupReply.Data
	}
	companyId, companyName, companyError := service.cloudCompany(ctx, querier, hardwareId)
	if companyError != nil {
		return LicenceLookupView{}, companyError
	}
	lookupView.CompanyId = companyId
	lookupView.CompanyName = companyName
	return lookupView, nil
}

func (service *Service) LicencePackages(ctx context.Context) ([]LicencePackageView, error) {
	packagesReply := struct {
		Data []LicencePackageView `json:"data"`
	}{}
	callError := service.wapangaji.call(ctx, http.MethodGet, "/payments/balce/packages/", "", nil, &packagesReply)
	if callError != nil {
		return nil, callError
	}
	if packagesReply.Data == nil {
		return []LicencePackageView{}, nil
	}
	return packagesReply.Data, nil
}

func (service *Service) LicencePayments(ctx context.Context, querier database.Querier, sessionToken string, hardwareId string) ([]LicencePaymentView, error) {
	historyReply := struct {
		Data []LicencePaymentView `json:"data"`
	}{}
	historyError := service.withWapangaji(ctx, querier, sessionToken, func(accessToken string) error {
		return service.wapangaji.call(ctx, http.MethodGet, "/payments/balce/admin/payments/"+licenceQuery(strings.TrimSpace(hardwareId), ""), accessToken, nil, &historyReply)
	})
	if historyError != nil {
		return nil, historyError
	}
	if historyReply.Data == nil {
		return []LicencePaymentView{}, nil
	}
	return historyReply.Data, nil
}

func (service *Service) RecordPayment(ctx context.Context, querier database.Querier, staff Staff, sessionToken string, request RecordPaymentRequest) (LicenceView, error) {
	hardwareId := strings.TrimSpace(request.HardwareId)
	companyId, _, companyError := service.cloudCompany(ctx, querier, hardwareId)
	if companyError != nil {
		return LicenceView{}, companyError
	}

	paymentBody := map[string]any{
		"hardware_id": hardwareId,
		"phone":       strings.TrimSpace(request.Phone),
		"package_id":  request.PackageId,
		"amount":      request.Amount,
		"method":      request.Method,
		"reference":   strings.TrimSpace(request.Reference),
	}
	paymentReply := struct {
		Data LicenceView `json:"data"`
	}{}
	recordError := service.withWapangaji(ctx, querier, sessionToken, func(accessToken string) error {
		return service.wapangaji.call(ctx, http.MethodPost, "/payments/balce/admin/record-payment/", accessToken, paymentBody, &paymentReply)
	})
	if recordError != nil {
		return LicenceView{}, recordError
	}

	refreshNote := ""
	if companyId != nil && service.subscriptionsService != nil {
		tenantError := database.SetTenant(ctx, querier, service.isPostgres, *companyId)
		if tenantError != nil {
			return LicenceView{}, tenantError
		}
		_, refreshError := service.subscriptionsService.Refresh(ctx, querier, *companyId)
		if refreshError != nil {
			refreshNote = " · the shop picks it up on its next check"
		}
	}

	packageName := ""
	if paymentReply.Data.Package != nil {
		packageName = *paymentReply.Data.Package
	}
	auditDetails := fmt.Sprintf("%s · %v %s · %s · until %s%s", packageName, request.Amount, request.Method, hardwareId, paymentReply.Data.ExpiresAt, refreshNote)
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "recorded_payment", companyId, auditDetails, time.Now().UTC())
	if auditError != nil {
		return LicenceView{}, auditError
	}
	return paymentReply.Data, nil
}

func (service *Service) LicenceDevices(ctx context.Context, querier database.Querier, sessionToken string, hardwareId string) (LicenceDevicesView, error) {
	devicesReply := struct {
		Data LicenceDevicesView `json:"data"`
	}{}
	devicesError := service.withWapangaji(ctx, querier, sessionToken, func(accessToken string) error {
		return service.wapangaji.call(ctx, http.MethodGet, "/payments/balce/admin/devices/"+licenceQuery(strings.TrimSpace(hardwareId), ""), accessToken, nil, &devicesReply)
	})
	if devicesError != nil {
		return LicenceDevicesView{}, asUpdateRequired(devicesError)
	}
	if devicesReply.Data.HardwareIds == nil {
		devicesReply.Data.HardwareIds = []string{}
	}
	return devicesReply.Data, nil
}

func (service *Service) RemoveDevice(ctx context.Context, querier database.Querier, staff Staff, sessionToken string, request RemoveDeviceRequest) (LicenceDevicesView, error) {
	removeBody := map[string]string{
		"hardware_id":        strings.TrimSpace(request.HardwareId),
		"remove_hardware_id": strings.TrimSpace(request.RemoveHardwareId),
	}
	removeReply := struct {
		Data LicenceDevicesView `json:"data"`
	}{}
	removeError := service.withWapangaji(ctx, querier, sessionToken, func(accessToken string) error {
		return service.wapangaji.call(ctx, http.MethodPost, "/payments/balce/admin/remove-device/", accessToken, removeBody, &removeReply)
	})
	if removeError != nil {
		return LicenceDevicesView{}, asUpdateRequired(removeError)
	}

	companyId, _, companyError := service.cloudCompany(ctx, querier, request.HardwareId)
	if companyError != nil {
		return LicenceDevicesView{}, companyError
	}
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "removed_device", companyId, removeBody["remove_hardware_id"], time.Now().UTC())
	if auditError != nil {
		return LicenceDevicesView{}, auditError
	}
	if removeReply.Data.HardwareIds == nil {
		removeReply.Data.HardwareIds = []string{}
	}
	return removeReply.Data, nil
}

func asUpdateRequired(callError error) error {
	wapangajiError := &WapangajiError{}
	isMissingEndpoint := errors.As(callError, &wapangajiError) && wapangajiError.StatusCode == http.StatusNotFound && strings.HasPrefix(wapangajiError.Message, "Wapangaji answered")
	if isMissingEndpoint {
		return ErrWapangajiUpdateRequired
	}
	return callError
}

func (service *Service) cloudCompany(ctx context.Context, querier database.Querier, hardwareId string) (*uuid.UUID, *string, error) {
	isCloudDevice := strings.HasPrefix(hardwareId, cloudDevicePrefix)
	if !isCloudDevice {
		return nil, nil, nil
	}
	companyId, parseError := uuid.Parse(strings.TrimPrefix(hardwareId, cloudDevicePrefix))
	if parseError != nil {
		return nil, nil, nil
	}
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return nil, nil, openError
	}
	shopDetail, findError := service.repository.FindShop(ctx, querier, companyId, time.Now().UTC())
	if findError != nil {
		return nil, nil, findError
	}
	if shopDetail == nil {
		return nil, nil, nil
	}
	return &companyId, &shopDetail.Name, nil
}
