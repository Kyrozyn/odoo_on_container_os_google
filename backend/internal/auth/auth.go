package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// ChangePassword hashes and stores a new admin password.
func (m *Manager) ChangePassword(newPassword string) error {
	if len(newPassword) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return m.store.Update(func(st *models.State) error {
		st.Settings.PasswordHash = string(hash)
		st.Settings.PasswordChanged = true
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
	return &claims, nil
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
