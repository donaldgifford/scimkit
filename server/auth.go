package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/donaldgifford/scimkit/scim"
)

// Auth guards every request and describes itself for the
// ServiceProviderConfig authenticationSchemes list. A nil Config.Auth
// disables the layer — bring your own middleware around the handler.
type Auth interface {
	// Wrap applies the guard to next; unauthenticated requests must be
	// answered without calling next.
	Wrap(next http.Handler) http.Handler
	// Scheme is advertised in ServiceProviderConfig.
	Scheme() AuthScheme
}

// AuthScheme is one entry of ServiceProviderConfig's
// authenticationSchemes (RFC 7643 §5).
type AuthScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	SpecURI          string `json:"specUri,omitempty"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// BearerToken authenticates requests with the OAuth bearer scheme
// (RFC 6750): verify receives the token from the Authorization header and
// returns nil to admit the request. Missing or rejected credentials are
// answered with a SCIM 401 body and a WWW-Authenticate challenge.
//
// verify sees the raw presented token. When it compares against a known
// secret it must do so in constant time (crypto/subtle) so response
// timing does not leak the secret byte by byte; StaticBearerToken does
// exactly that for the single-shared-secret case.
func BearerToken(verify func(ctx context.Context, token string) error) Auth {
	return &bearerAuth{verify: verify, logger: slog.Default()}
}

// errTokenMismatch is the rejection StaticBearerToken's verify returns;
// it never reaches the wire, which only ever sees the generic 401.
var errTokenMismatch = errors.New("server: bearer token mismatch")

// StaticBearerToken admits requests presenting exactly token, compared in
// constant time. It is the right Auth for a single IdP tenant holding one
// shared secret; an empty token rejects every request rather than
// admitting all of them.
func StaticBearerToken(token string) Auth {
	want := []byte(token)
	return BearerToken(func(_ context.Context, got string) error {
		if len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			return errTokenMismatch
		}
		return nil
	})
}

type bearerAuth struct {
	verify func(ctx context.Context, token string) error
	logger *slog.Logger
}

var _ Auth = (*bearerAuth)(nil)

func (b *bearerAuth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || b.verify(r.Context(), token) != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
			writeError(w, b.logger, &scim.Error{
				Status: http.StatusUnauthorized,
				Detail: "authentication required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (*bearerAuth) Scheme() AuthScheme {
	return AuthScheme{
		Type:        "oauthbearertoken",
		Name:        "OAuth Bearer Token",
		Description: "Authentication scheme using the OAuth Bearer Token standard",
		SpecURI:     "https://www.rfc-editor.org/info/rfc6750",
		Primary:     true,
	}
}

// useLogger lets the server thread its configured logger into the auth
// layer at wiring time, matching loggerAware.
func (b *bearerAuth) useLogger(l *slog.Logger) {
	if l != nil {
		b.logger = l
	}
}

// loggerAware is implemented by Auth values that can adopt the server's
// configured logger, so unauthenticated responses log consistently
// rather than falling back to the global default.
type loggerAware interface {
	useLogger(*slog.Logger)
}

// bearerToken extracts the token from an Authorization header value,
// matching the scheme case-insensitively per RFC 9110 §11.1.
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
