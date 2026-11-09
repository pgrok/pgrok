package database

import (
	"context"
	"time"

	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/strutil"
)

// Principal represents a user.
type Principal struct {
	ID          int64
	Identifier  string
	DisplayName string
	Token       string
	Subdomain   string
	LastTCPPort int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (*Principal) TableName() string {
	return "principals"
}

type UpsertPrincipalOptions struct {
	Identifier  string
	DisplayName string
	Subdomain   string
}

// newPrincipalToken generates a new random tunnel token.
func newPrincipalToken() string {
	return cryptoutil.SHA1(strutil.MustRandomChars(10))
}

// UpsertPrincipal upserts a principal with given options. A new principal is
// assigned a freshly generated token.
func (db *DB) UpsertPrincipal(ctx context.Context, opts UpsertPrincipalOptions) (*Principal, error) {
	p := &Principal{
		Identifier:  opts.Identifier,
		DisplayName: opts.DisplayName,
		Token:       newPrincipalToken(),
		Subdomain:   opts.Subdomain,
	}
	return p, db.WithContext(ctx).Where("identifier = ?", opts.Identifier).FirstOrCreate(p).Error
}

// GetPrincipalByID returns a principal with given id.
func (db *DB) GetPrincipalByID(ctx context.Context, id int64) (*Principal, error) {
	var p Principal
	return &p, db.WithContext(ctx).Where("id = ?", id).First(&p).Error
}

// GetPrincipalByToken returns a principal with given token.
func (db *DB) GetPrincipalByToken(ctx context.Context, token string) (*Principal, error) {
	var p Principal
	return &p, db.WithContext(ctx).Where("token = ?", token).First(&p).Error
}

// UpdatePrincipalLastTCPPort updates the last TCP port of the principal.
func (db *DB) UpdatePrincipalLastTCPPort(ctx context.Context, id int64, port int) error {
	return db.WithContext(ctx).Model(&Principal{}).Where("id = ?", id).Update("last_tcp_port", port).Error
}

// RegeneratePrincipalToken replaces the token of the principal with a freshly
// generated one and returns the new token. The old token stops authenticating
// new connections immediately.
func (db *DB) RegeneratePrincipalToken(ctx context.Context, id int64) (string, error) {
	token := newPrincipalToken()
	return token, db.WithContext(ctx).Model(&Principal{}).Where("id = ?", id).Update("token", token).Error
}
