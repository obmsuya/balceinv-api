package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) OpenPlatformRead(ctx context.Context, querier database.Querier, isPostgres bool) error {
	if !isPostgres {
		return nil
	}
	_, setFlagError := querier.ExecContext(ctx, `SELECT set_config('app.platform_admin', 'on', true)`)
	if setFlagError != nil {
		return fmt.Errorf("failed to open platform reading: %w", setFlagError)
	}
	return nil
}

func (repository *Repository) FindStaffByEmail(ctx context.Context, querier database.Querier, email string) (*Staff, error) {
	query := `SELECT id, email, name, password_hash, totp_secret, role, is_active FROM platform_staff WHERE email = $1`
	return scanStaff(querier.QueryRowContext(ctx, query, email))
}

func (repository *Repository) FindStaffBySession(ctx context.Context, querier database.Querier, tokenHash string, now time.Time) (*Staff, error) {
	query := `
		SELECT st.id, st.email, st.name, st.password_hash, st.totp_secret, st.role, st.is_active
		FROM platform_sessions ps
		JOIN platform_staff st ON st.id = ps.staff_id
		WHERE ps.token_hash = $1 AND ps.expires_at > $2 AND st.is_active
	`
	return scanStaff(querier.QueryRowContext(ctx, query, tokenHash, now))
}

func scanStaff(staffRow *sql.Row) (*Staff, error) {
	staff := Staff{}
	scanError := staffRow.Scan(&staff.Id, &staff.Email, &staff.Name, &staff.PasswordHash, &staff.TotpSecret, &staff.Role, &staff.IsActive)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the staff member: %w", scanError)
	}
	return &staff, nil
}

func (repository *Repository) SaveStaff(ctx context.Context, querier database.Querier, staff Staff, now time.Time) error {
	query := `
		INSERT INTO platform_staff (id, email, name, password_hash, totp_secret, role, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $7)
		ON CONFLICT (email) DO UPDATE
		SET name = excluded.name, password_hash = excluded.password_hash, totp_secret = excluded.totp_secret,
		    role = excluded.role, is_active = TRUE, updated_at = excluded.updated_at
	`
	_, saveError := querier.ExecContext(ctx, query, staff.Id, staff.Email, staff.Name, staff.PasswordHash, staff.TotpSecret, staff.Role, now)
	if saveError != nil {
		return fmt.Errorf("failed to save the staff member: %w", saveError)
	}
	return nil
}

func (repository *Repository) InsertSession(ctx context.Context, querier database.Querier, tokenHash string, staffId uuid.UUID, expiresAt time.Time, now time.Time) error {
	_, insertError := querier.ExecContext(ctx, `INSERT INTO platform_sessions (token_hash, staff_id, expires_at, created_at) VALUES ($1, $2, $3, $4)`, tokenHash, staffId, expiresAt, now)
	if insertError != nil {
		return fmt.Errorf("failed to start the admin session: %w", insertError)
	}
	_, touchError := querier.ExecContext(ctx, `UPDATE platform_staff SET last_signed_in_at = $2 WHERE id = $1`, staffId, now)
	if touchError != nil {
		return fmt.Errorf("failed to record the sign-in: %w", touchError)
	}
	return nil
}

func (repository *Repository) DeleteSession(ctx context.Context, querier database.Querier, tokenHash string) error {
	_, deleteError := querier.ExecContext(ctx, `DELETE FROM platform_sessions WHERE token_hash = $1`, tokenHash)
	if deleteError != nil {
		return fmt.Errorf("failed to end the admin session: %w", deleteError)
	}
	return nil
}

func (repository *Repository) InsertAudit(ctx context.Context, querier database.Querier, staffId uuid.UUID, action string, companyId *uuid.UUID, details string, now time.Time) error {
	query := `INSERT INTO platform_audit (id, staff_id, action, company_id, details, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, insertError := querier.ExecContext(ctx, query, uuid.Must(uuid.NewV7()), staffId, action, companyId, details, now)
	if insertError != nil {
		return fmt.Errorf("failed to write the audit log: %w", insertError)
	}
	return nil
}

func (repository *Repository) ListAudit(ctx context.Context, querier database.Querier, companyId *uuid.UUID, limit int) ([]AuditView, error) {
	query := `
		SELECT a.id, st.name, a.action, a.company_id, c.name, a.details, a.created_at
		FROM platform_audit a
		JOIN platform_staff st ON st.id = a.staff_id
		LEFT JOIN companies c ON c.id = a.company_id
		WHERE ($1::uuid IS NULL OR a.company_id = $1::uuid)
		ORDER BY a.created_at DESC
		LIMIT $2
	`
	auditRows, queryError := querier.QueryContext(ctx, query, companyId, limit)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the audit log: %w", queryError)
	}
	defer auditRows.Close()

	auditViews := []AuditView{}
	for auditRows.Next() {
		auditView := AuditView{}
		scanError := auditRows.Scan(&auditView.Id, &auditView.StaffName, &auditView.Action, &auditView.CompanyId, &auditView.CompanyName, &auditView.Details, &auditView.CreatedAt)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an audit entry: %w", scanError)
		}
		auditViews = append(auditViews, auditView)
	}
	return auditViews, auditRows.Err()
}

const shopRowSelect = `
	SELECT c.id, c.name, c.phone,
	       (SELECT u.name FROM users u JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id WHERE u.company_id = c.id AND r.is_owner ORDER BY u.created_at LIMIT 1),
	       (SELECT u.email FROM users u JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id WHERE u.company_id = c.id AND r.is_owner ORDER BY u.created_at LIMIT 1),
	       (SELECT COUNT(*) FROM users u WHERE u.company_id = c.id),
	       (SELECT COUNT(*) FROM shops sh WHERE sh.company_id = c.id),
	       (SELECT COUNT(*) FROM sales s WHERE s.company_id = c.id AND s.created_at >= $1 AND s.voided_at IS NULL),
	       (SELECT MAX(s.created_at) FROM sales s WHERE s.company_id = c.id AND s.voided_at IS NULL),
	       cs.is_trial, cs.expires_at, c.created_at, c.currency_code
	FROM companies c
	LEFT JOIN company_subscriptions cs ON cs.company_id = c.id
`

func scanShopRow(scanner interface{ Scan(...any) error }, shopRow *ShopRowView, currencyCode *string) error {
	return scanner.Scan(&shopRow.CompanyId, &shopRow.Name, &shopRow.Phone, &shopRow.OwnerName, &shopRow.OwnerEmail, &shopRow.UserCount,
		&shopRow.ShopCount, &shopRow.SalesLast30Days, &shopRow.LastSaleAt, &shopRow.IsTrial, &shopRow.SubscriptionEnds, &shopRow.CreatedAt, currencyCode)
}

func (repository *Repository) ListShops(ctx context.Context, querier database.Querier, searchText string, since time.Time, limit int, offset int) (ShopPage, error) {
	pattern := "%" + strings.ToLower(strings.TrimSpace(searchText)) + "%"
	whereClause := ` WHERE lower(c.name) LIKE $2 OR EXISTS (SELECT 1 FROM users u WHERE u.company_id = c.id AND u.email LIKE $2)`

	totalShops := int64(0)
	countError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM companies c`+strings.ReplaceAll(whereClause, "$2", "$1"), pattern).Scan(&totalShops)
	if countError != nil {
		return ShopPage{}, fmt.Errorf("failed to count shops: %w", countError)
	}

	query := shopRowSelect + whereClause + ` ORDER BY c.created_at DESC LIMIT $3 OFFSET $4`
	shopRows, queryError := querier.QueryContext(ctx, query, since, pattern, limit, offset)
	if queryError != nil {
		return ShopPage{}, fmt.Errorf("failed to list shops: %w", queryError)
	}
	defer shopRows.Close()

	shopPage := ShopPage{Items: []ShopRowView{}, Total: totalShops}
	for shopRows.Next() {
		shopRow := ShopRowView{}
		currencyCode := ""
		scanError := scanShopRow(shopRows, &shopRow, &currencyCode)
		if scanError != nil {
			return ShopPage{}, fmt.Errorf("failed to scan a shop: %w", scanError)
		}
		shopPage.Items = append(shopPage.Items, shopRow)
	}
	return shopPage, shopRows.Err()
}

func (repository *Repository) FindShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time) (*ShopDetailView, error) {
	shopDetail := ShopDetailView{ShopNames: []string{}, Users: []ShopUserView{}}
	scanError := scanShopRow(querier.QueryRowContext(ctx, shopRowSelect+` WHERE c.id = $2`, since, companyId), &shopDetail.ShopRowView, &shopDetail.CurrencyCode)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the shop: %w", scanError)
	}

	nameRows, namesError := querier.QueryContext(ctx, `SELECT name FROM shops WHERE company_id = $1 ORDER BY name`, companyId)
	if namesError != nil {
		return nil, fmt.Errorf("failed to list the business's shops: %w", namesError)
	}
	for nameRows.Next() {
		shopName := ""
		nameScanError := nameRows.Scan(&shopName)
		if nameScanError != nil {
			nameRows.Close()
			return nil, fmt.Errorf("failed to scan a shop name: %w", nameScanError)
		}
		shopDetail.ShopNames = append(shopDetail.ShopNames, shopName)
	}
	nameRows.Close()

	userQuery := `
		SELECT u.id, u.name, u.email, r.name, r.is_owner, u.is_active, u.must_change_password
		FROM users u
		JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id
		WHERE u.company_id = $1
		ORDER BY r.is_owner DESC, u.name
	`
	userRows, usersError := querier.QueryContext(ctx, userQuery, companyId)
	if usersError != nil {
		return nil, fmt.Errorf("failed to list the business's users: %w", usersError)
	}
	defer userRows.Close()
	for userRows.Next() {
		shopUser := ShopUserView{}
		userScanError := userRows.Scan(&shopUser.Id, &shopUser.Name, &shopUser.Email, &shopUser.RoleName, &shopUser.IsOwner, &shopUser.IsActive, &shopUser.MustChangePassword)
		if userScanError != nil {
			return nil, fmt.Errorf("failed to scan a user: %w", userScanError)
		}
		shopDetail.Users = append(shopDetail.Users, shopUser)
	}
	return &shopDetail, userRows.Err()
}

func (repository *Repository) FindSubscription(ctx context.Context, querier database.Querier, companyId uuid.UUID) (bool, *bool, *time.Time, error) {
	isTrial := false
	expiresAt := time.Time{}
	scanError := querier.QueryRowContext(ctx, `SELECT is_trial, expires_at FROM company_subscriptions WHERE company_id = $1`, companyId).Scan(&isTrial, &expiresAt)
	if errors.Is(scanError, sql.ErrNoRows) {
		return false, nil, nil, nil
	}
	if scanError != nil {
		return false, nil, nil, fmt.Errorf("failed to read the subscription: %w", scanError)
	}
	return true, &isTrial, &expiresAt, nil
}

func (repository *Repository) SetTrialEnd(ctx context.Context, querier database.Querier, companyId uuid.UUID, hasRow bool, trialEndsAt time.Time, daysGranted int, now time.Time) error {
	query := `UPDATE company_subscriptions SET expires_at = $2, days_granted = days_granted + $3, updated_at = $4 WHERE company_id = $1 AND is_trial`
	if !hasRow {
		query = `INSERT INTO company_subscriptions (company_id, license_key, expires_at, days_granted, max_devices, is_trial, created_at, updated_at) VALUES ($1, 'trial', $2, $3, 1, TRUE, $4, $4)`
	}
	_, saveError := querier.ExecContext(ctx, query, companyId, trialEndsAt, daysGranted, now)
	if saveError != nil {
		return fmt.Errorf("failed to extend the trial: %w", saveError)
	}
	return nil
}

func (repository *Repository) ResetUserPassword(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, passwordHash string, now time.Time) (string, error) {
	userEmail := ""
	updateQuery := `
		UPDATE users SET password_hash = $3, must_change_password = TRUE, updated_at = $4
		WHERE company_id = $1 AND id = $2
		RETURNING email
	`
	scanError := querier.QueryRowContext(ctx, updateQuery, companyId, userId, passwordHash, now).Scan(&userEmail)
	if errors.Is(scanError, sql.ErrNoRows) {
		return "", nil
	}
	if scanError != nil {
		return "", fmt.Errorf("failed to reset the password: %w", scanError)
	}
	_, endSessionsError := querier.ExecContext(ctx, `DELETE FROM sessions WHERE company_id = $1 AND user_id = $2`, companyId, userId)
	if endSessionsError != nil {
		return "", fmt.Errorf("failed to sign the user out: %w", endSessionsError)
	}
	return userEmail, nil
}

func (repository *Repository) ListSupportMessages(ctx context.Context, querier database.Querier, onlyOpen bool, limit int) ([]SupportMessageView, error) {
	query := `
		SELECT m.id, m.company_id, c.name, u.name, m.topic, m.message, m.contact_email, m.contact_phone, m.status, m.created_at,
		       m.handled_at, st.name
		FROM support_messages m
		JOIN companies c ON c.id = m.company_id
		LEFT JOIN users u ON u.company_id = m.company_id AND u.id = m.user_id
		LEFT JOIN platform_staff st ON st.id = m.handled_by
		WHERE ($1 = FALSE OR m.handled_at IS NULL)
		ORDER BY m.created_at DESC
		LIMIT $2
	`
	messageRows, queryError := querier.QueryContext(ctx, query, onlyOpen, limit)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list support messages: %w", queryError)
	}
	defer messageRows.Close()

	messages := []SupportMessageView{}
	for messageRows.Next() {
		message := SupportMessageView{}
		scanError := messageRows.Scan(&message.Id, &message.CompanyId, &message.CompanyName, &message.UserName, &message.Topic, &message.Message,
			&message.ContactEmail, &message.ContactPhone, &message.EmailStatus, &message.CreatedAt, &message.HandledAt, &message.HandledByName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a support message: %w", scanError)
		}
		messages = append(messages, message)
	}
	return messages, messageRows.Err()
}

func (repository *Repository) FindSupportCompany(ctx context.Context, querier database.Querier, messageId uuid.UUID) (*uuid.UUID, error) {
	companyId := uuid.UUID{}
	scanError := querier.QueryRowContext(ctx, `SELECT company_id FROM support_messages WHERE id = $1`, messageId).Scan(&companyId)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the support message: %w", scanError)
	}
	return &companyId, nil
}

func (repository *Repository) MarkSupportHandled(ctx context.Context, querier database.Querier, companyId uuid.UUID, messageId uuid.UUID, staffId uuid.UUID, now time.Time) error {
	query := `UPDATE support_messages SET handled_at = $3, handled_by = $4 WHERE company_id = $1 AND id = $2 AND handled_at IS NULL`
	_, updateError := querier.ExecContext(ctx, query, companyId, messageId, now, staffId)
	if updateError != nil {
		return fmt.Errorf("failed to mark the message handled: %w", updateError)
	}
	return nil
}

func daysText(days int) string {
	return strconv.Itoa(days) + " days"
}
