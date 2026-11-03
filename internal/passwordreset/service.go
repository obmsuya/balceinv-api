package passwordreset

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/auth"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/mailer"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/google/uuid"
)

const deliveryTimeout = 2 * mailer.SmtpTimeout

type Service struct {
	openDatabase    *database.Database
	repository      *Repository
	authRepository  *auth.Repository
	usersRepository *users.Repository
	mailServer      mailer.Server
	smsApiKey       string
}

func NewService(openDatabase *database.Database, loadedConfig *config.Config) *Service {
	return &Service{
		openDatabase:    openDatabase,
		repository:      NewRepository(),
		authRepository:  auth.NewRepository(),
		usersRepository: users.NewRepository(),
		mailServer:      mailer.ServerFrom(loadedConfig),
		smsApiKey:       loadedConfig.BalceSmsApiKey,
	}
}

func (service *Service) SendCode(ctx context.Context, rawEmail string) error {
	candidate, findError := service.findCandidate(ctx, users.NormalizeEmail(rawEmail))
	if findError != nil {
		return findError
	}
	if candidate == nil || !candidate.IsActive {
		return nil
	}

	plainCode, codeError := newCode()
	if codeError != nil {
		return codeError
	}

	createdAt := time.Now().UTC()
	recipient := Recipient{}
	writeError := service.inTenant(ctx, candidate.CompanyId, func(writeTransaction database.Querier) error {
		recentCodes, countError := service.repository.CountSince(ctx, writeTransaction, candidate.CompanyId, candidate.UserId, createdAt.Add(-time.Hour))
		if countError != nil {
			return countError
		}
		if recentCodes >= codesPerHour {
			return errCodeQuotaUsed
		}

		retireError := service.repository.RetireUnused(ctx, writeTransaction, candidate.CompanyId, candidate.UserId, createdAt)
		if retireError != nil {
			return retireError
		}
		newResetCode := ResetCode{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: candidate.CompanyId,
			UserId:    candidate.UserId,
			CodeHash:  hashCode(candidate.UserId, plainCode),
			ExpiresAt: createdAt.Add(codeLifetime),
			CreatedAt: createdAt,
		}
		insertError := service.repository.Insert(ctx, writeTransaction, newResetCode)
		if insertError != nil {
			return insertError
		}

		foundRecipient, recipientError := service.repository.FindRecipient(ctx, writeTransaction, candidate.CompanyId, candidate.UserId)
		recipient = foundRecipient
		return recipientError
	})
	if errors.Is(writeError, errCodeQuotaUsed) {
		slog.Warn("password reset code not sent: hourly limit reached", "userId", candidate.UserId)
		return nil
	}
	if writeError != nil {
		return writeError
	}

	deliveryContext, cancelDelivery := context.WithTimeout(context.WithoutCancel(ctx), deliveryTimeout)
	go func() {
		defer cancelDelivery()
		service.deliver(deliveryContext, recipient, plainCode)
	}()
	return nil
}

func (service *Service) Reset(ctx context.Context, request ResetRequest) error {
	candidate, findError := service.findCandidate(ctx, users.NormalizeEmail(request.Email))
	if findError != nil {
		return findError
	}
	if candidate == nil || !candidate.IsActive {
		return ErrInvalidCode
	}
	if len([]byte(request.NewPassword)) > 72 {
		return ErrPasswordTooLong
	}

	isCodeWrong := false
	checkedAt := time.Now().UTC()
	writeError := service.inTenant(ctx, candidate.CompanyId, func(writeTransaction database.Querier) error {
		liveCode, liveError := service.repository.FindLive(ctx, writeTransaction, candidate.CompanyId, candidate.UserId, checkedAt)
		if liveError != nil {
			return liveError
		}
		if liveCode == nil || liveCode.Attempts >= codeMaxAttempts {
			return ErrInvalidCode
		}

		codeMatches := subtle.ConstantTimeCompare([]byte(liveCode.CodeHash), []byte(hashCode(candidate.UserId, request.Code))) == 1
		if !codeMatches {
			isCodeWrong = true
			return service.repository.CountAttempt(ctx, writeTransaction, candidate.CompanyId, liveCode.Id)
		}

		passwordHash, hashError := security.HashPassword(request.NewPassword)
		if hashError != nil {
			return fmt.Errorf("failed to hash the new password: %w", hashError)
		}
		updateError := service.usersRepository.UpdatePasswordHash(ctx, writeTransaction, candidate.CompanyId, candidate.UserId, passwordHash)
		if updateError != nil {
			return updateError
		}
		usedError := service.repository.MarkUsed(ctx, writeTransaction, candidate.CompanyId, liveCode.Id, checkedAt)
		if usedError != nil {
			return usedError
		}
		return service.usersRepository.RevokeSessions(ctx, writeTransaction, candidate.CompanyId, candidate.UserId, uuid.Nil)
	})
	if writeError != nil {
		return writeError
	}
	if isCodeWrong {
		return ErrInvalidCode
	}
	return nil
}

func (service *Service) deliver(ctx context.Context, recipient Recipient, plainCode string) {
	emailError := sendCodeEmail(ctx, service.mailServer, recipient, plainCode)
	if emailError != nil {
		slog.Warn("password reset email not sent", "error", emailError)
	}
	// ponytail: the SMS goes to the business phone for every person, so a staff code reaches the owner. Add a phone per user when staff should get their own.
	hasBusinessPhone := recipient.BusinessPhone != nil && *recipient.BusinessPhone != ""
	if hasBusinessPhone {
		smsError := sendCodeSms(ctx, service.smsApiKey, *recipient.BusinessPhone, plainCode)
		if smsError != nil {
			slog.Warn("password reset SMS not sent", "error", smsError)
		}
	}
}

func (service *Service) findCandidate(ctx context.Context, normalizedEmail string) (*auth.LoginCandidate, error) {
	isPostgres := service.openDatabase.IsPostgres()
	lookupTransaction, beginError := service.openDatabase.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: isPostgres})
	if beginError != nil {
		return nil, fmt.Errorf("failed to begin the reset lookup: %w", beginError)
	}
	defer lookupTransaction.Rollback()

	enableLookupError := database.SetAuthLookup(ctx, lookupTransaction, isPostgres, true)
	if enableLookupError != nil {
		return nil, enableLookupError
	}
	return service.authRepository.FindLoginCandidate(ctx, lookupTransaction, normalizedEmail)
}

func (service *Service) inTenant(ctx context.Context, companyId uuid.UUID, write func(writeTransaction database.Querier) error) error {
	writeTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("failed to begin the reset write: %w", beginError)
	}
	defer writeTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, writeTransaction, service.openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return setTenantError
	}
	writeError := write(writeTransaction)
	if writeError != nil {
		return writeError
	}
	return writeTransaction.Commit()
}

func newCode() (string, error) {
	randomNumber, randomError := rand.Int(rand.Reader, big.NewInt(1000000))
	if randomError != nil {
		return "", fmt.Errorf("failed to make a reset code: %w", randomError)
	}
	return fmt.Sprintf("%06d", randomNumber.Int64()), nil
}

func hashCode(userId uuid.UUID, plainCode string) string {
	codeHash := sha256.Sum256([]byte(userId.String() + ":" + plainCode))
	return hex.EncodeToString(codeHash[:])
}
