package oidcprovider

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/pgrok/pgrok/internal/database"
)

const (
	// DevicePath is the page where users enter the user code of a device
	// authorization request (RFC 8628 "verification_uri").
	DevicePath = "/device"

	deviceAuthorizationLifetime = 10 * time.Minute
	devicePollInterval          = 5 * time.Second
	// deviceMinPollInterval is the shortest interval between polls that is not
	// answered with "slow_down", leaving room for network jitter.
	deviceMinPollInterval = devicePollInterval - time.Second
)

// deviceScopes are the scopes a device can request. Others are dropped, since
// the device authorization endpoint does not validate them.
var deviceScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeOfflineAccess}

var _ op.DeviceAuthorizationStorage = (*Storage)(nil)

// NormalizeUserCode returns the user code as stored, so that users can enter it
// in any case and with or without its dash.
func NormalizeUserCode(userCode string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(userCode)))
}

func (s *Storage) StoreDeviceAuthorization(ctx context.Context, clientID, deviceCode, userCode string, expires time.Time, scopes []string) error {
	scopes = slices.DeleteFunc(slices.Clone(scopes), func(scope string) bool {
		return !slices.Contains(deviceScopes, scope)
	})
	err := s.db.CreateOIDCDeviceAuthorization(ctx, &database.OIDCDeviceAuthorization{
		ID:        hashToken(deviceCode),
		UserCode:  NormalizeUserCode(userCode),
		ClientID:  clientID,
		Scopes:    nonNil(scopes),
		ExpiresAt: expires,
	})
	if errors.Is(err, database.ErrOIDCDuplicateUserCode) {
		return op.ErrDuplicateUserCode
	}
	return err
}

// GetDeviceAuthorizatonState is called on every poll of the token endpoint.
// Polling faster than the interval returns context.DeadlineExceeded, which the
// provider answers with "slow_down" (RFC 8628, section 3.5).
func (s *Storage) GetDeviceAuthorizatonState(ctx context.Context, clientID, deviceCode string) (*op.DeviceAuthorizationState, error) {
	auth, err := s.db.PollOIDCDeviceAuthorization(ctx, hashToken(deviceCode), clientID, deviceMinPollInterval)
	if errors.Is(err, database.ErrOIDCDevicePolledTooFast) {
		return nil, errors.WithMessage(context.DeadlineExceeded, "polled too fast")
	} else if err != nil {
		return nil, err
	}
	return &op.DeviceAuthorizationState{
		ClientID: auth.ClientID,
		Scopes:   auth.Scopes,
		Expires:  auth.ExpiresAt,
		Done:     auth.Done,
		Denied:   auth.Denied,
		Subject:  auth.Subject,
		AuthTime: auth.AuthTime,
	}, nil
}
