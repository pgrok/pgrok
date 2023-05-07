package database

import (
	"context"
	"time"
)

// Principal represents a user.
type Principal struct {
	ID          int64  `gorm:"primaryKey"`
	Identifier  string `gorm:"unique;not null"`
	DisplayName string `gorm:"not null"`
	Token       string `gorm:"unique;not null"`
	Subdomain   string `gorm:"unique;not null"`
	LastTCPPort int
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (*Principal) TableName() string {
	return "principles"
}

type UpsertPrincipalOptions struct {
	Identifier  string
	DisplayName string
	Token       string
	Subdomain   string
}

// UpsertPrincipal upserts a principle with given options.
func (db *DB) UpsertPrincipal(ctx context.Context, opts UpsertPrincipalOptions) (*Principal, error) {
	p := &Principal{
		Identifier:  opts.Identifier,
		DisplayName: opts.DisplayName,
		Token:       opts.Token,
		Subdomain:   opts.Subdomain,
	}
	return p, db.WithContext(ctx).Where("identifier = ?", opts.Identifier).FirstOrCreate(p).Error
}

// GetPrincipalByID returns a principle with given id.
func (db *DB) GetPrincipalByID(ctx context.Context, id int64) (*Principal, error) {
	var p Principal
	return &p, db.WithContext(ctx).Where("id = ?", id).First(&p).Error
}

// GetPrincipalByToken returns a principle with given token.
func (db *DB) GetPrincipalByToken(ctx context.Context, token string) (*Principal, error) {
	var p Principal
	return &p, db.WithContext(ctx).Where("token = ?", token).First(&p).Error
}

// UpdatePrincipalLastTCPPort updates the last TCP port of the principal.
func (db *DB) UpdatePrincipalLastTCPPort(ctx context.Context, id int64, port int) error {
	return db.WithContext(ctx).Model(&Principal{}).Where("id = ?", id).Update("last_tcp_port", port).Error
}
