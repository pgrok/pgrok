package database

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrOIDCRecordNotFound is returned when an OIDC provider record does not
// exist, has expired, or was already used.
var ErrOIDCRecordNotFound = errors.New("record not found")

// OIDCKey is a key of the OpenID Provider. Keys with use "sig" sign ID tokens,
// and the key with use "enc" encrypts opaque access tokens.
type OIDCKey struct {
	ID        string    `gorm:"primaryKey"`
	Use       string    `gorm:"not null;index"`
	Algorithm string    `gorm:"not null"`
	Material  []byte    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func (*OIDCKey) TableName() string { return "oidc_keys" }

// ListOIDCKeys returns the keys with the given use, newest first.
func (db *DB) ListOIDCKeys(ctx context.Context, use string) ([]*OIDCKey, error) {
	var keys []*OIDCKey
	return keys, db.WithContext(ctx).Where("use = ?", use).Order("created_at DESC").Find(&keys).Error
}

// CreateOIDCKeyIfNotExists creates the key unless a key with the same ID exists.
func (db *DB) CreateOIDCKeyIfNotExists(ctx context.Context, key *OIDCKey) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(key).Error
}

// OIDCAuthRequest is an authorization request waiting for the user to sign in
// and approve it, and then for the client to redeem its code.
type OIDCAuthRequest struct {
	ID                  string   `gorm:"primaryKey"`
	ClientID            string   `gorm:"not null"`
	RedirectURI         string   `gorm:"not null"`
	State               string   `gorm:"not null"`
	Nonce               string   `gorm:"not null"`
	Scopes              []string `gorm:"serializer:json;not null"`
	ResponseType        string   `gorm:"not null"`
	ResponseMode        string   `gorm:"not null"`
	CodeChallenge       string   `gorm:"not null"`
	CodeChallengeMethod string   `gorm:"not null"`
	Prompt              []string `gorm:"serializer:json;not null"`
	// MaxAge is the "max_age" parameter in seconds, or nil when absent.
	// "prompt=login" sets it to 0.
	MaxAge    *uint
	LoginHint string `gorm:"not null"`
	// CodeHash is the SHA-256 of the authorization code, set once the request is
	// approved.
	CodeHash *string `gorm:"unique"`
	// CallbackHash is the SHA-256 of the one-time secret that the approving
	// browser presents at the provider's callback to receive the code, set once
	// the request is approved.
	CallbackHash *string `gorm:"unique"`
	PrincipalID  int64   `gorm:"not null"`
	AuthTime     time.Time
	Done         bool      `gorm:"not null"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time `gorm:"not null"`
}

func (*OIDCAuthRequest) TableName() string { return "oidc_auth_requests" }

// CreateOIDCAuthRequest creates the authorization request.
func (db *DB) CreateOIDCAuthRequest(ctx context.Context, req *OIDCAuthRequest) error {
	return db.WithContext(ctx).Create(req).Error
}

// GetOIDCAuthRequestByID returns the unexpired authorization request with the
// given ID.
func (db *DB) GetOIDCAuthRequestByID(ctx context.Context, id string) (*OIDCAuthRequest, error) {
	var req OIDCAuthRequest
	err := db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOIDCRecordNotFound
	}
	return &req, err
}

// RedeemOIDCAuthRequestCode returns the unexpired, approved authorization
// request whose code has the given hash, and clears the code so that it can be
// redeemed only once, even by concurrent requests.
func (db *DB) RedeemOIDCAuthRequestCode(ctx context.Context, codeHash string) (*OIDCAuthRequest, error) {
	return db.redeemOIDCAuthRequest(ctx, "code_hash", codeHash)
}

// RedeemOIDCAuthRequestCallback returns the unexpired, approved authorization
// request whose callback secret has the given hash, and clears it so that only
// one browser receives a code for the request.
func (db *DB) RedeemOIDCAuthRequestCallback(ctx context.Context, callbackHash string) (*OIDCAuthRequest, error) {
	return db.redeemOIDCAuthRequest(ctx, "callback_hash", callbackHash)
}

func (db *DB) redeemOIDCAuthRequest(ctx context.Context, column, hash string) (*OIDCAuthRequest, error) {
	var reqs []*OIDCAuthRequest
	err := db.WithContext(ctx).Model(&reqs).
		Clauses(clause.Returning{}).
		Where(column+" = ? AND done AND expires_at > ?", hash, time.Now()).
		Update(column, nil).Error
	if err != nil {
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, ErrOIDCRecordNotFound
	}
	return reqs[0], nil
}

// SetOIDCAuthRequestCodeHash stores the hash of the code issued for the request.
func (db *DB) SetOIDCAuthRequestCodeHash(ctx context.Context, id, codeHash string) error {
	return db.WithContext(ctx).Model(&OIDCAuthRequest{}).Where("id = ?", id).Update("code_hash", codeHash).Error
}

// CompleteOIDCAuthRequest marks the request as approved by the principal, who
// signed in at authTime. A request can be approved only once, so that nobody
// else who learns its ID can replace the principal before the client redeems
// the code. Only the browser that holds the callback secret whose hash is
// callbackHash can then collect the code.
func (db *DB) CompleteOIDCAuthRequest(ctx context.Context, id string, principalID int64, authTime time.Time, callbackHash string) error {
	result := db.WithContext(ctx).Model(&OIDCAuthRequest{}).
		Where("id = ? AND NOT done AND expires_at > ?", id, time.Now()).
		Updates(map[string]any{
			"principal_id":  principalID,
			"auth_time":     authTime,
			"callback_hash": callbackHash,
			"done":          true,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOIDCRecordNotFound
	}
	return nil
}

// DeleteOIDCAuthRequest deletes the authorization request.
func (db *DB) DeleteOIDCAuthRequest(ctx context.Context, id string) error {
	return db.WithContext(ctx).Where("id = ?", id).Delete(&OIDCAuthRequest{}).Error
}

// OIDCAccessToken is an issued opaque access token.
type OIDCAccessToken struct {
	ID             string    `gorm:"primaryKey"`
	ClientID       string    `gorm:"not null;index"`
	Subject        string    `gorm:"not null;index"`
	Audience       []string  `gorm:"serializer:json;not null"`
	Scopes         []string  `gorm:"serializer:json;not null"`
	RefreshTokenID string    `gorm:"not null;index"`
	ExpiresAt      time.Time `gorm:"not null;index"`
	CreatedAt      time.Time `gorm:"not null"`
}

func (*OIDCAccessToken) TableName() string { return "oidc_access_tokens" }

// CreateOIDCAccessToken creates the access token.
func (db *DB) CreateOIDCAccessToken(ctx context.Context, token *OIDCAccessToken) error {
	return db.WithContext(ctx).Create(token).Error
}

// GetOIDCAccessToken returns the unexpired access token with the given ID.
func (db *DB) GetOIDCAccessToken(ctx context.Context, id string) (*OIDCAccessToken, error) {
	var token OIDCAccessToken
	err := db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOIDCRecordNotFound
	}
	return &token, err
}

// DeleteOIDCAccessToken deletes the access token.
func (db *DB) DeleteOIDCAccessToken(ctx context.Context, id string) error {
	return db.WithContext(ctx).Where("id = ?", id).Delete(&OIDCAccessToken{}).Error
}

// OIDCRefreshToken is an issued refresh token. Each use rotates it: the used
// token is kept, marked as used, so that presenting it again revokes every
// token in its family (RFC 9700, section 4.14.2).
type OIDCRefreshToken struct {
	// ID is the SHA-256 of the token.
	ID            string    `gorm:"primaryKey"`
	FamilyID      string    `gorm:"not null;index"`
	ClientID      string    `gorm:"not null;index"`
	Subject       string    `gorm:"not null;index"`
	Audience      []string  `gorm:"serializer:json;not null"`
	Scopes        []string  `gorm:"serializer:json;not null"`
	AMR           []string  `gorm:"serializer:json;not null"`
	AuthTime      time.Time `gorm:"not null"`
	AccessTokenID string    `gorm:"not null"`
	Used          bool      `gorm:"not null"`
	UsedAt        time.Time
	ExpiresAt     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time `gorm:"not null"`
}

func (*OIDCRefreshToken) TableName() string { return "oidc_refresh_tokens" }

// GetOIDCRefreshToken returns the unexpired refresh token with the given ID,
// including one that was already used.
func (db *DB) GetOIDCRefreshToken(ctx context.Context, id string) (*OIDCRefreshToken, error) {
	var token OIDCRefreshToken
	err := db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOIDCRecordNotFound
	}
	return &token, err
}

// RotateOIDCRefreshToken marks the current refresh token as used, deletes the
// access token issued with it, and stores the next access and refresh tokens.
// It fails with ErrOIDCRecordNotFound when the current token was used
// concurrently.
func (db *DB) RotateOIDCRefreshToken(ctx context.Context, currentID string, access *OIDCAccessToken, next *OIDCRefreshToken) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current OIDCRefreshToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND used = ? AND expires_at > ?", currentID, false, time.Now()).
			First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrOIDCRecordNotFound
		} else if err != nil {
			return err
		}

		err = tx.Model(&current).Updates(map[string]any{"used": true, "used_at": time.Now()}).Error
		if err != nil {
			return errors.Wrap(err, "mark used")
		}
		err = tx.Where("id = ?", current.AccessTokenID).Delete(&OIDCAccessToken{}).Error
		if err != nil {
			return errors.Wrap(err, "delete previous access token")
		}

		next.FamilyID = current.FamilyID
		if err = tx.Create(access).Error; err != nil {
			return errors.Wrap(err, "create access token")
		}
		return tx.Create(next).Error
	})
}

// CreateOIDCTokens stores a new access token and, when it is not nil, the
// refresh token issued with it.
func (db *DB) CreateOIDCTokens(ctx context.Context, access *OIDCAccessToken, refresh *OIDCRefreshToken) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(access).Error; err != nil {
			return err
		}
		if refresh == nil {
			return nil
		}
		return tx.Create(refresh).Error
	})
}

// DeleteOIDCRefreshTokenFamily deletes every refresh token in the family and the
// access tokens issued with them.
func (db *DB) DeleteOIDCRefreshTokenFamily(ctx context.Context, familyID string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Wait for a rotation in progress to commit, so that the deletes below,
		// which each read the latest committed rows, include the tokens it adds.
		var ids []string
		err := tx.Model(&OIDCRefreshToken{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("family_id = ?", familyID).Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		err = tx.Where("refresh_token_id IN (?)",
			tx.Model(&OIDCRefreshToken{}).Select("id").Where("family_id = ?", familyID),
		).Delete(&OIDCAccessToken{}).Error
		if err != nil {
			return err
		}
		return tx.Where("family_id = ?", familyID).Delete(&OIDCRefreshToken{}).Error
	})
}

// DeleteOIDCTokensBySubjectAndClient deletes every token the client holds for
// the subject.
func (db *DB) DeleteOIDCTokensBySubjectAndClient(ctx context.Context, subject, clientID string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Wait for a rotation in progress to commit, as in
		// DeleteOIDCRefreshTokenFamily, so that the deletes include its tokens.
		var ids []string
		err := tx.Model(&OIDCRefreshToken{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("subject = ? AND client_id = ?", subject, clientID).Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		err = tx.Where("subject = ? AND client_id = ?", subject, clientID).Delete(&OIDCAccessToken{}).Error
		if err != nil {
			return err
		}
		return tx.Where("subject = ? AND client_id = ?", subject, clientID).Delete(&OIDCRefreshToken{}).Error
	})
}

// expiringOIDCModels are the OIDC provider records with an "expires_at" column.
var expiringOIDCModels = []any{&OIDCAuthRequest{}, &OIDCAccessToken{}, &OIDCRefreshToken{}}

// DeleteExpiredOIDCRecords deletes expired OIDC provider records.
func (db *DB) DeleteExpiredOIDCRecords(ctx context.Context) error {
	now := time.Now()
	for _, model := range expiringOIDCModels {
		err := db.WithContext(ctx).Where("expires_at <= ?", now).Delete(model).Error
		if err != nil {
			return errors.Wrapf(err, "delete expired %T", model)
		}
	}
	return nil
}
