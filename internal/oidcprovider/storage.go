package oidcprovider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strconv"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/pgrok/pgrok/internal/database"
)

const (
	authRequestLifetime  = 10 * time.Minute
	accessTokenLifetime  = time.Hour
	refreshTokenLifetime = 30 * 24 * time.Hour
	// refreshTokenReuseGrace is how long after a refresh token was rotated it
	// can be presented again without revoking its family. Clients that share a
	// refresh token, such as two pgrok processes started together, can race to
	// rotate it, and the loser is not an attacker.
	refreshTokenReuseGrace = 10 * time.Second
	idTokenLifetime        = time.Hour
)

// Storage implements op.Storage on the pgrokd database. Subjects are principal
// IDs.
type Storage struct {
	db          *database.DB
	clients     map[string]*Client
	signingKeys []*signingKey
}

var (
	_ op.Storage                   = (*Storage)(nil)
	_ op.ClientCredentialsStorage  = (*Storage)(nil)
	_ op.CanSetUserinfoFromRequest = (*Storage)(nil)
)

// Subject returns the OIDC subject of the principal.
func Subject(principalID int64) string {
	return strconv.FormatInt(principalID, 10)
}

// hashToken returns the hex SHA-256 of a code or token, which is what the
// database stores in place of the secret itself.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	if slices.Contains(req.Prompt, oidc.PromptNone) {
		// The provider never sees the browser session, so it cannot sign the
		// user in without interaction.
		return nil, oidc.ErrLoginRequired()
	}

	authReq := &database.OIDCAuthRequest{
		ID:                  uuid.NewString(),
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		Nonce:               req.Nonce,
		Scopes:              req.Scopes,
		ResponseType:        string(req.ResponseType),
		ResponseMode:        string(req.ResponseMode),
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: string(req.CodeChallengeMethod),
		Prompt:              req.Prompt,
		LoginHint:           req.LoginHint,
		MaxAge:              req.MaxAge,
		ExpiresAt:           time.Now().Add(authRequestLifetime),
	}
	if authReq.Scopes == nil {
		authReq.Scopes = []string{}
	}
	if authReq.Prompt == nil {
		authReq.Prompt = []string{}
	}
	if err := s.db.CreateOIDCAuthRequest(ctx, authReq); err != nil {
		return nil, err
	}
	return &authRequest{authReq}, nil
}

func (s *Storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	req, err := s.db.GetOIDCAuthRequestByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &authRequest{req}, nil
}

// AuthRequestByCode redeems the code, so a second token request with the same
// code fails.
func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	req, err := s.db.RedeemOIDCAuthRequestCode(ctx, hashToken(code))
	if err != nil {
		return nil, err
	}
	return &authRequest{req}, nil
}

func (s *Storage) SaveAuthCode(ctx context.Context, id, code string) error {
	return s.db.SetOIDCAuthRequestCodeHash(ctx, id, hashToken(code))
}

func (s *Storage) DeleteAuthRequest(ctx context.Context, id string) error {
	return s.db.DeleteOIDCAuthRequest(ctx, id)
}

// tokenInfo is what the stored tokens record about the grant they come from.
type tokenInfo struct {
	clientID string
	authTime time.Time
	amr      []string
}

func tokenInfoFromRequest(request op.TokenRequest) (tokenInfo, error) {
	switch req := request.(type) {
	case *authRequest:
		return tokenInfo{clientID: req.ClientID, authTime: req.GetAuthTime(), amr: req.GetAMR()}, nil
	case *refreshTokenRequest:
		return tokenInfo{clientID: req.ClientID, authTime: req.AuthTime, amr: req.AMR}, nil
	case *oidc.JWTTokenRequest:
		// Client credentials grant, where the client is the subject.
		return tokenInfo{clientID: req.Subject, authTime: time.Now()}, nil
	default:
		return tokenInfo{}, errors.Errorf("unsupported token request %T", request)
	}
}

func newAccessToken(request op.TokenRequest, clientID, refreshTokenID string) *database.OIDCAccessToken {
	return &database.OIDCAccessToken{
		ID:             uuid.NewString(),
		ClientID:       clientID,
		Subject:        request.GetSubject(),
		Audience:       nonNil(request.GetAudience()),
		Scopes:         nonNil(request.GetScopes()),
		RefreshTokenID: refreshTokenID,
		ExpiresAt:      time.Now().Add(accessTokenLifetime),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	info, err := tokenInfoFromRequest(request)
	if err != nil {
		return "", time.Time{}, err
	}
	token := newAccessToken(request, info.clientID, "")
	if err = s.db.CreateOIDCTokens(ctx, token, nil); err != nil {
		return "", time.Time{}, err
	}
	return token.ID, token.ExpiresAt, nil
}

func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (accessTokenID, newRefreshToken string, expiration time.Time, err error) {
	info, err := tokenInfoFromRequest(request)
	if err != nil {
		return "", "", time.Time{}, err
	}
	newRefreshToken, err = randomToken()
	if err != nil {
		return "", "", time.Time{}, errors.Wrap(err, "generate refresh token")
	}
	refreshTokenID := hashToken(newRefreshToken)
	access := newAccessToken(request, info.clientID, refreshTokenID)
	// A refresh request can narrow the scopes of the access token, but the new
	// refresh token keeps those of the grant (RFC 6749, section 6).
	scopes := access.Scopes
	if r, ok := request.(*refreshTokenRequest); ok {
		scopes = nonNil(r.Scopes)
	}
	refresh := &database.OIDCRefreshToken{
		ID:            refreshTokenID,
		FamilyID:      uuid.NewString(),
		ClientID:      info.clientID,
		Subject:       request.GetSubject(),
		Audience:      access.Audience,
		Scopes:        scopes,
		AMR:           nonNil(info.amr),
		AuthTime:      info.authTime,
		AccessTokenID: access.ID,
		ExpiresAt:     time.Now().Add(refreshTokenLifetime),
	}

	if currentRefreshToken == "" {
		err = s.db.CreateOIDCTokens(ctx, access, refresh)
	} else {
		err = s.db.RotateOIDCRefreshToken(ctx, hashToken(currentRefreshToken), access, refresh)
		if errors.Is(err, database.ErrOIDCRecordNotFound) {
			// Another request rotated the token concurrently.
			return "", "", time.Time{}, oidc.ErrInvalidGrant().WithDescription("refresh token was already used")
		}
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	return access.ID, newRefreshToken, access.ExpiresAt, nil
}

// TokenRequestByRefreshToken returns the grant of an unused refresh token. A
// refresh token that was already rotated means it leaked, so presenting it
// revokes every token issued from the same grant.
func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	token, err := s.db.GetOIDCRefreshToken(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, err
	}
	if token.Used {
		if time.Since(token.UsedAt) > refreshTokenReuseGrace {
			if err = s.db.DeleteOIDCRefreshTokenFamily(ctx, token.FamilyID); err != nil {
				return nil, errors.Wrap(err, "revoke reused refresh token family")
			}
		}
		return nil, errors.New("refresh token was already used")
	}
	return &refreshTokenRequest{OIDCRefreshToken: token}, nil
}

type endWebSessionKey struct{}

// WithEndWebSession returns a copy of ctx in which an accepted end_session
// request also calls endWebSession with the subject of its id_token_hint, or ""
// without one, so that the web UI can sign that user out.
func WithEndWebSession(ctx context.Context, endWebSession func(subject string)) context.Context {
	return context.WithValue(ctx, endWebSessionKey{}, endWebSession)
}

func (s *Storage) TerminateSession(ctx context.Context, userID, clientID string) error {
	if err := s.db.DeleteOIDCTokensBySubjectAndClient(ctx, userID, clientID); err != nil {
		return err
	}
	if endWebSession, ok := ctx.Value(endWebSessionKey{}).(func(string)); ok {
		endWebSession(userID)
	}
	return nil
}

func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (userID, tokenID string, err error) {
	refresh, err := s.db.GetOIDCRefreshToken(ctx, hashToken(token))
	if errors.Is(err, database.ErrOIDCRecordNotFound) {
		return "", "", op.ErrInvalidRefreshToken
	} else if err != nil {
		return "", "", err
	}
	return refresh.Subject, refresh.ID, nil
}

// RevokeToken revokes an access token by its ID, or a refresh token by its hash
// as returned by GetRefreshTokenInfo. Revoking a refresh token also revokes the
// access tokens issued from the same grant (RFC 7009, section 2.1).
func (s *Storage) RevokeToken(ctx context.Context, tokenOrTokenID, _, clientID string) *oidc.Error {
	access, err := s.db.GetOIDCAccessToken(ctx, tokenOrTokenID)
	if err == nil {
		if access.ClientID != clientID {
			return oidc.ErrInvalidClient().WithDescription("token was not issued for this client")
		}
		if err = s.db.DeleteOIDCAccessToken(ctx, access.ID); err != nil {
			return oidc.ErrServerError().WithParent(err)
		}
		return nil
	} else if !errors.Is(err, database.ErrOIDCRecordNotFound) {
		return oidc.ErrServerError().WithParent(err)
	}

	// A refresh token arrives as the hash from GetRefreshTokenInfo, or as the
	// token itself when the request hinted that it is an access token.
	refresh, err := s.db.GetOIDCRefreshToken(ctx, tokenOrTokenID)
	if errors.Is(err, database.ErrOIDCRecordNotFound) {
		refresh, err = s.db.GetOIDCRefreshToken(ctx, hashToken(tokenOrTokenID))
	}
	if errors.Is(err, database.ErrOIDCRecordNotFound) {
		// Unknown tokens are already as revoked as they can be (RFC 7009,
		// section 2.2).
		return nil
	} else if err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	if refresh.ClientID != clientID {
		return oidc.ErrInvalidClient().WithDescription("token was not issued for this client")
	}
	if err = s.db.DeleteOIDCRefreshTokenFamily(ctx, refresh.FamilyID); err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	return nil
}

// SigningKey returns the newest signing key.
func (s *Storage) SigningKey(context.Context) (op.SigningKey, error) {
	return s.signingKeys[0], nil
}

func (s *Storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

// KeySet returns the public halves of every signing key, so that tokens signed
// by a previous key still verify after a new one is added.
func (s *Storage) KeySet(context.Context) ([]op.Key, error) {
	keys := make([]op.Key, 0, len(s.signingKeys))
	for _, key := range s.signingKeys {
		keys = append(keys, publicKey{key})
	}
	return keys, nil
}

func (s *Storage) GetClientByClientID(_ context.Context, clientID string) (op.Client, error) {
	client, ok := s.clients[clientID]
	if !ok {
		return nil, errors.Errorf("client %q not found", clientID)
	}
	return client, nil
}

func (s *Storage) AuthorizeClientIDSecret(_ context.Context, clientID, clientSecret string) error {
	client, ok := s.clients[clientID]
	if !ok || !client.authenticate(clientSecret) {
		return errors.New("invalid client credentials")
	}
	return nil
}

func (s *Storage) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}

func (s *Storage) SetUserinfoFromRequest(ctx context.Context, userinfo *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	return s.setUserinfo(ctx, userinfo, request.GetSubject(), scopes)
}

func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, _, _ string) error {
	token, err := s.db.GetOIDCAccessToken(ctx, tokenID)
	if err != nil {
		return err
	}
	return s.setUserinfo(ctx, userinfo, token.Subject, token.Scopes)
}

// SetIntrospectionFromToken answers introspection requests from the client
// that the token was issued to.
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, introspection *oidc.IntrospectionResponse, tokenID, _, clientID string) error {
	token, err := s.db.GetOIDCAccessToken(ctx, tokenID)
	if err != nil {
		return err
	}
	if !slices.Contains(token.Audience, clientID) {
		return errors.New("token is not valid for this client")
	}

	userinfo := new(oidc.UserInfo)
	if err = s.setUserinfo(ctx, userinfo, token.Subject, token.Scopes); err != nil {
		return err
	}
	introspection.SetUserInfo(userinfo)
	introspection.Scope = token.Scopes
	introspection.ClientID = token.ClientID
	introspection.Expiration = oidc.FromTime(token.ExpiresAt)
	introspection.IssuedAt = oidc.FromTime(token.CreatedAt)
	return nil
}

// setUserinfo sets the claims of the subject that the scopes grant. Subjects of
// the client credentials grant are clients, which have no claims.
func (s *Storage) setUserinfo(ctx context.Context, userinfo *oidc.UserInfo, subject string, scopes []string) error {
	userinfo.Subject = subject
	if _, ok := s.clients[subject]; ok {
		return nil
	}
	principalID, err := strconv.ParseInt(subject, 10, 64)
	if err != nil {
		return errors.Errorf("invalid subject %q", subject)
	}
	principal, err := s.db.GetPrincipalByID(ctx, principalID)
	if err != nil {
		return errors.Wrap(err, "get principal")
	}
	if slices.Contains(scopes, oidc.ScopeProfile) {
		userinfo.Name = principal.DisplayName
		userinfo.PreferredUsername = principal.Identifier
	}
	return nil
}

func (s *Storage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}

// GetKeyByIDAndClientID rejects every key, because no client authenticates with
// private_key_jwt.
func (s *Storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errors.New("JWT client authentication is not supported")
}

// ValidateJWTProfileScopes rejects every JWT profile grant (RFC 7523), which no
// client is registered for.
func (s *Storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errors.New("JWT profile grant is not supported")
}

func (s *Storage) Health(ctx context.Context) error {
	sqlDB, err := s.db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (s *Storage) ClientCredentials(_ context.Context, clientID, clientSecret string) (op.Client, error) {
	client, ok := s.clients[clientID]
	if !ok || !client.authenticate(clientSecret) || !slices.Contains(client.grantTypes, oidc.GrantTypeClientCredentials) {
		return nil, errors.New("invalid client credentials")
	}
	return client, nil
}

func (s *Storage) ClientCredentialsTokenRequest(_ context.Context, clientID string, scopes []string) (op.TokenRequest, error) {
	return &oidc.JWTTokenRequest{
		Subject:  clientID,
		Audience: []string{clientID},
		Scopes:   scopes,
	}, nil
}

// authRequest adapts a stored authorization request to op.AuthRequest.
type authRequest struct {
	*database.OIDCAuthRequest
}

var _ op.AuthRequest = (*authRequest)(nil)

func (r *authRequest) GetID() string  { return r.ID }
func (r *authRequest) GetACR() string { return "" }

// GetAMR is empty: users sign in at the upstream identity provider, which does
// not tell pgrokd how they authenticated.
func (r *authRequest) GetAMR() []string { return nil }

func (r *authRequest) GetAudience() []string  { return []string{r.ClientID} }
func (r *authRequest) GetAuthTime() time.Time { return r.AuthTime }
func (r *authRequest) GetClientID() string    { return r.ClientID }

func (r *authRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if r.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{
		Challenge: r.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(r.CodeChallengeMethod),
	}
}

func (r *authRequest) GetNonce() string                   { return r.Nonce }
func (r *authRequest) GetRedirectURI() string             { return r.RedirectURI }
func (r *authRequest) GetResponseType() oidc.ResponseType { return oidc.ResponseType(r.ResponseType) }
func (r *authRequest) GetResponseMode() oidc.ResponseMode { return oidc.ResponseMode(r.ResponseMode) }
func (r *authRequest) GetScopes() []string                { return r.Scopes }
func (r *authRequest) GetState() string                   { return r.State }
func (r *authRequest) GetSubject() string {
	if !r.OIDCAuthRequest.Done {
		return ""
	}
	return Subject(r.PrincipalID)
}
func (r *authRequest) Done() bool { return r.OIDCAuthRequest.Done }

// refreshTokenRequest adapts a stored refresh token to op.RefreshTokenRequest.
type refreshTokenRequest struct {
	*database.OIDCRefreshToken
	// currentScopes are the scopes the client asked for when refreshing, or nil
	// when it asked for all of them.
	currentScopes []string
}

var _ op.RefreshTokenRequest = (*refreshTokenRequest)(nil)

func (r *refreshTokenRequest) GetAMR() []string       { return r.AMR }
func (r *refreshTokenRequest) GetAudience() []string  { return r.Audience }
func (r *refreshTokenRequest) GetAuthTime() time.Time { return r.AuthTime }
func (r *refreshTokenRequest) GetClientID() string    { return r.ClientID }
func (r *refreshTokenRequest) GetScopes() []string {
	if r.currentScopes != nil {
		return r.currentScopes
	}
	return r.Scopes
}
func (r *refreshTokenRequest) GetSubject() string          { return r.Subject }
func (r *refreshTokenRequest) SetCurrentScopes(s []string) { r.currentScopes = s }
