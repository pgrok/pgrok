package database

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"

	"github.com/pgrok/pgrok/internal/cryptoutil"
)

// HostKey represents a host key for SSH.
type HostKey struct {
	Algorithm cryptoutil.KeyAlgorithm `gorm:"unique; not null"`
	PEM       []byte                  `gorm:"not null"`
	CreatedAt time.Time               `gorm:"not null"`
}

var ErrHostKeyNotExists = errors.New("host key does not exist")

// GetHostKeyByAlgorithm returns a host key with given algorithm.
func (db *DB) GetHostKeyByAlgorithm(ctx context.Context, algorithm cryptoutil.KeyAlgorithm) (*HostKey, error) {
	var k HostKey
	err := db.WithContext(ctx).Where("algorithm = ?", algorithm).First(&k).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrHostKeyNotExists
		}
		return nil, err
	}
	return &k, nil
}

// CreateHostKey creates a host key with given algorithm and PEM data.
func (db *DB) CreateHostKey(ctx context.Context, algorithm cryptoutil.KeyAlgorithm, pem []byte) (*HostKey, error) {
	k := &HostKey{
		Algorithm: algorithm,
		PEM:       pem,
	}
	return k, db.WithContext(ctx).Create(k).Error
}
