package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/adriangitvitz/yoru/interpreter"
	"github.com/adriangitvitz/yoru/parser"
)

// JWTMiddleware verifies an HMAC-SHA256 Bearer token, stamps the claims onto
// req.context["jwt_claims"], or short-circuits with 401.
//
// Token shape (matches service/auth.yr's jwt_sign):
//
//	header  = base64url(JSON.encode({"alg":"HS256","typ":"JWT"}))
//	payload = base64url(JSON.encode(claims))
//	sig     = base64url(hex(HMAC-SHA256(secret, header + "." + payload)))
//
// The signature is base64url(*hex*), not base64url(raw bytes) — a Yoru-only
// quirk. For RFC 7519-compatible tokens, sign externally and adapt this verifier.
func JWTMiddleware(secret string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				writeUnauthorized(w)
				return
			}
			token := strings.TrimPrefix(authz, "Bearer ")
			claims, ok := verifyJWT(token, secret)
			if !ok {
				writeUnauthorized(w)
				return
			}
			r = SetRequestContext(r, "jwt_claims", claimsToMap(claims))
			next.ServeHTTP(w, r)
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

// verifyJWT returns decoded claims on success. All failures return (nil, false)
// without distinguishing the cause — avoids token-shape oracle attacks.
func verifyJWT(token, secret string) (*interpreter.ObjectVal, bool) {
	if secret == "" {
		// Fail closed: an unconfigured secret must not accept every request.
		return nil, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	signingInput := parts[0] + "." + parts[1]

	// Match auth.yr: base64url(hex(HMAC-SHA256(secret, signingInput))).
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(
		[]byte(hex.EncodeToString(mac.Sum(nil))),
	)
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return nil, false
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var raw map[string]any
	if err := json.Unmarshal(payloadJSON, &raw); err != nil {
		return nil, false
	}

	// exp is mandatory and must be in the future. Missing exp → fail closed.
	expRaw, ok := raw["exp"]
	if !ok {
		return nil, false
	}
	var exp int64
	switch v := expRaw.(type) {
	case float64:
		exp = int64(v)
	case int64:
		exp = v
	default:
		return nil, false
	}
	if exp < time.Now().Unix() {
		return nil, false
	}

	// Return ObjectVal here for the unit-test helper; the middleware re-wraps
	// as MapVal before stamping onto req.context so handlers can use claims.get(k).
	fields := make(map[string]interpreter.Value, len(raw))
	for k, v := range raw {
		fields[k] = jsonClaimValue(v)
	}
	return &interpreter.ObjectVal{
		TypeName: "JWTClaims",
		Fields:   fields,
	}, true
}

// claimsToMap converts verifyJWT's ObjectVal into a MapVal for req.context;
// Go callers keep the typed object, handlers get dynamic .get(k).
func claimsToMap(claims *interpreter.ObjectVal) *interpreter.MapVal {
	entries := make(map[string]interpreter.Value, len(claims.Fields))
	var order []string
	for k, v := range claims.Fields {
		entries[k] = v
		order = append(order, k)
	}
	return &interpreter.MapVal{Entries: entries, Order: order}
}

func jsonClaimValue(v any) interpreter.Value {
	switch x := v.(type) {
	case string:
		return &interpreter.StringVal{V: x}
	case float64:
		if x == float64(int64(x)) {
			return &interpreter.IntVal{V: int64(x)}
		}
		return &interpreter.FloatVal{V: x}
	case bool:
		return &interpreter.BoolVal{V: x}
	case nil:
		return &interpreter.NilVal{}
	}
	return &interpreter.NilVal{}
}

func init() {
	RegisterMiddleware("JWT", func(ref parser.MiddlewareRef, interp *interpreter.Interpreter) Middleware {
		var secret string
		if ref.Method == "verify" {
			if s, ok := middlewareStringArg(ref, 0, interp); ok {
				secret = s
			}
		}
		// `JWT` bare or `JWT.verify` without args → fail-closed middleware.
		return JWTMiddleware(secret)
	})
}
