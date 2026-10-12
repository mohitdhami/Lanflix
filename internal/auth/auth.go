package auth

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Password string `json:"-"`
	Active   bool   `json:"-"`
}

func Secret() string {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return s
	}
	return "lanflix-local-secret-change-me"
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(pw, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// SignToken mirrors the Node format {id,email,role,iat,exp} so tokens
// issued by either backend validate on both.
func SignToken(id int64, email, role string) (string, error) {
	claims := jwt.MapClaims{
		"id":    id,
		"email": email,
		"role":  role,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(Secret()))
}

func ParseToken(token string) (id int64, email, role string, ok bool) {
	t, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, good := t.Method.(*jwt.SigningMethodHMAC); !good {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(Secret()), nil
	})
	if err != nil || !t.Valid {
		return 0, "", "", false
	}
	m, _ := t.Claims.(jwt.MapClaims)
	idf, _ := m["id"].(float64)
	em, _ := m["email"].(string)
	ro, _ := m["role"].(string)
	if idf == 0 || em == "" {
		return 0, "", "", false
	}
	return int64(idf), em, ro, true
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(h, "Bearer "), true
}

// RequireAuth resolves the user; RequireAdmin additionally enforces role.
func RequireAuth(next func(http.ResponseWriter, *http.Request, int64, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearer(r)
		if !ok {
			writeErr(w, 401, "No token")
			return
		}
		id, _, role, good := ParseToken(tok)
		if !good {
			writeErr(w, 401, "Invalid token")
			return
		}
		next(w, r, id, role)
	}
}
