package auth

import (
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(password string) (string, error) {
	hashed_password, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}
	return hashed_password, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {
	doesPasswordsMatch, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}
	return doesPasswordsMatch, nil
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	uuidString := userID.String()
	now := time.Now().UTC()
	expiresWhen := now.Add(expiresIn)
	numericIssueDate := jwt.NewNumericDate(now)
	numericExpireDate := jwt.NewNumericDate(expiresWhen)

	newJWT := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.RegisteredClaims{
			Issuer:    "chirpy",
			IssuedAt:  numericIssueDate,
			ExpiresAt: numericExpireDate,
			Subject:   uuidString,
		},
	)
	secretBytes := []byte(tokenSecret)
	tokenString, err := newJWT.SignedString(secretBytes)
	return tokenString, err
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claimsStruct := jwt.RegisteredClaims{} // An empty struct to hold the parsed claims

	token, err := jwt.ParseWithClaims(
		tokenString,   // The token string to parse
		&claimsStruct, // A pointer to where the parsed claims will go
		func(token *jwt.Token) (interface{}, error) { // The keyFunc
			return []byte(tokenSecret), nil // Returns the secret for validation
		},
	)
	if err != nil {
		return uuid.Nil, err
	}
	subject, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, err
	}
	uuidObj, err := uuid.Parse(subject)
	if err != nil {
		return uuid.Nil, err
	}
	issuer, err := token.Claims.GetIssuer()
	if err != nil {
		return uuid.Nil, err
	}
	if issuer != "chirpy" {
		return uuid.Nil, errors.New("Invalid Issuer")
	}

	return uuidObj, err
}
