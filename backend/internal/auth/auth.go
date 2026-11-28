package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/runodoo/runodoo/internal/models"
	"github.com/runodoo/runodoo/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type Claims struct {
	Sub string `json:"sub"`
	Ver int    `json:"ver"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

type Manager struct {
	store *store.Store
}

func NewManager(st *store.Store) *Manager {
	return &Manager{store: st}
}

// VerifyPassword checks plaintext password against stored bcrypt hash.
func (m *Manager) VerifyPassword(password string) bool {
	st := m.store.Get()
	err := bcrypt.CompareHashAndPassword([]byte(st.Settings.PasswordHash), []byte(password))
	return err == nil
}

// ChangePassword hashes and stores a new admin password and revokes existing sessions.
func (m *Manager) ChangePassword(newPassword string) error {
	if len(newPassword) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return m.store.Update(func(st *models.State) error {
		st.Settings.PasswordHash = string(hash)
		st.Settings.PasswordChanged = true
		st.Settings.TokenVersion++
		return nil
	})
}

// RevokeAllSessions increments TokenVersion so all previously issued JWTs become invalid.
func (m *Manager) RevokeAllSessions() error {
	return m.store.Update(func(st *models.State) error {
		st.Settings.TokenVersion++
		return nil
	})
}

// GenerateToken signs an HMAC-SHA256 JWT valid for 7 days.
func (m *Manager) GenerateToken() (string, error) {
	st := m.store.Get()
	now := time.Now()
	headerBytes, _ := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	claimsBytes, _ := json.Marshal(Claims{
		Sub: "admin",
		Ver: st.Settings.TokenVersion,
		Iat: now.Unix(),
		Exp: now.Add(7 * 24 * time.Hour).Unix(),
	})

	h := base64.RawURLEncoding.EncodeToString(headerBytes)
	p := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := h + "." + p

	mac := hmac.New(sha256.New, []byte(st.Settings.JWTSecret))
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

// ValidateToken verifies signature and expiration.
func (m *Manager) ValidateToken(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	st := m.store.Get()
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(st.Settings.JWTSecret))
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, errors.New("invalid token signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("token expired")
	}
	if claims.Ver != st.Settings.TokenVersion {
		return nil, errors.New("token revoked")
	}
	return &claims, nil
}

// GenerateTOTPSecret creates a new random Base32 TOTP secret and otpauth URI.
func GenerateTOTPSecret(issuer, account string) (secret, otpauthURI string, err error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	otpauthURI = fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		issuer, account, secret, issuer)
	return secret, otpauthURI, nil
}

// VerifyTOTPCode validates a 6-digit RFC 6238 TOTP code with +/- 1 step (30s) skew tolerance.
func VerifyTOTPCode(secret, code string) bool {
	return verifyTOTPAt(secret, code, time.Now())
}

func verifyTOTPAt(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return false
	}
	step := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		expected := totpAtCounter(key, uint64(step+offset))
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func totpAtCounter(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", bin%1000000)
}

// EnableTOTP verifies the user's code against secret and activates 2FA.
func (m *Manager) EnableTOTP(secret, code string) error {
	if !VerifyTOTPCode(secret, code) {
		return errors.New("invalid 2FA verification code")
	}
	return m.store.Update(func(st *models.State) error {
		st.Settings.TOTPSecret = strings.ToUpper(strings.TrimSpace(secret))
		st.Settings.TOTPEnabled = true
		return nil
	})
}

// DisableTOTP turns off 2FA and revokes existing sessions.
func (m *Manager) DisableTOTP() error {
	return m.store.Update(func(st *models.State) error {
		st.Settings.TOTPEnabled = false
		st.Settings.TOTPSecret = ""
		st.Settings.TokenVersion++
		return nil
	})
}

// VerifyTOTP checks the provided 6-digit code if 2FA is enabled.
func (m *Manager) VerifyTOTP(code string) bool {
	st := m.store.Get()
	if !st.Settings.TOTPEnabled || st.Settings.TOTPSecret == "" {
		return true
	}
	return VerifyTOTPCode(st.Settings.TOTPSecret, code)
}

// Middleware enforces JWT Bearer authentication (or ?token= query param for downloads).
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			token = q
		}

		if token == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if _, err := m.ValidateToken(token); err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
