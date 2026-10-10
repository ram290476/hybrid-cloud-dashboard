// Package devjwt mints and checks HMAC tokens for the local dev issuer.
// Cloud deployments validate Cognito JWTs instead; this package is the stand-in
// described in checklist step 11.
package devjwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims are the fields the backends read. sub is the Cognito user id.
type Claims struct {
	Sub string `json:"sub"`
	Iss string `json:"iss"`
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
}

// Mint returns a compact JWT signed with HS256.
func Mint(secret string, claims Claims) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := b64(header) + "." + b64(payload)
	mac := sign([]byte(unsigned), []byte(secret))
	return unsigned + "." + b64(mac), nil
}

// Parse checks the signature, expiry, issuer, and audience.
func Parse(secret, token, issuer, audience string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("malformed token")
	}
	mac := sign([]byte(parts[0]+"."+parts[1]), []byte(secret))
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(mac, got) {
		return Claims{}, errors.New("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return Claims{}, err
	}
	if issuer != "" && claims.Iss != issuer {
		return Claims{}, errors.New("bad issuer")
	}
	if audience != "" && claims.Aud != audience {
		return Claims{}, errors.New("bad audience")
	}
	if claims.Exp != 0 && !now.Before(time.Unix(claims.Exp, 0)) {
		return Claims{}, errors.New("expired")
	}
	if claims.Sub == "" {
		return Claims{}, errors.New("missing sub")
	}
	return claims, nil
}

func b64(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func sign(msg, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(msg)
	return mac.Sum(nil)
}
