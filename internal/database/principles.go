package database

import (
	"context"
	"time"
)

// Principle represents a user.
type Principle struct {
	ID          int64     `gorm:"primaryKey"`
	Identifier  string    `gorm:"unique;not null"`
	DisplayName string    `gorm:"not null"`
	Token       string    `gorm:"unique;not null"`
	Subdomain   string    `gorm:"unique;not null"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

type UpsertPrincipleOptions struct {
	Identifier  string
	DisplayName string
	Token       string
	Subdomain   string
}

// UpsertPrinciple upserts a principle with given options.
func (db *DB) UpsertPrinciple(ctx context.Context, opts UpsertPrincipleOptions) (*Principle, error) {
	p := &Principle{
		Identifier:  opts.Identifier,
		DisplayName: opts.DisplayName,
		Token:       opts.Token,
		Subdomain:   opts.Subdomain,
	}
	return p, db.WithContext(ctx).Where("identifier = ?", opts.Identifier).FirstOrCreate(p).Error
}

// GetPrincipleByID returns a principle with given id.
func (db *DB) GetPrincipleByID(ctx context.Context, id int64) (*Principle, error) {
	var p Principle
	return &p, db.WithContext(ctx).Where("id = ?", id).First(&p).Error
}

// GetPrincipleByToken returns a principle with given token.
func (db *DB) GetPrincipleByToken(ctx context.Context, token string) (*Principle, error) {
	var p Principle
	return &p, db.WithContext(ctx).Where("token = ?", token).First(&p).Error
}
