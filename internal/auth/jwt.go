package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Claims define a estrutura do payload de um token JWT seguro
type Claims struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	ProjectID string `json:"project_id"`
	Plan      string `json:"plan"`
	jwt.RegisteredClaims
}

// GenerateToken cria um novo token JWT assinado criptograficamente com HMAC-SHA256
func GenerateToken(userID, email, projectID, plan, secret string, duration time.Duration) (string, error) {
	claims := Claims{
		UserID:    userID,
		Email:     email,
		ProjectID: projectID,
		Plan:      plan,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("falha ao assinar JWT: %w", err)
	}

	return tokenString, nil
}

// ValidateToken decodifica e valida criptograficamente um token JWT recebido
func ValidateToken(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("token inválido: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("claims inválidas ou expiradas")
	}

	return claims, nil
}

// HashPassword calcula o hash bcrypt de uma senha fornecida
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("falha ao criptografar senha: %w", err)
	}
	return string(bytes), nil
}

// CheckPasswordHash compara uma senha com seu hash bcrypt correspondente
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
