package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMakeAndValidateJWT(t *testing.T) {
	userID := uuid.New() // A new UUID for our test user
	tokenSecret := "some-super-secret-key"
	expiresIn := time.Hour // Token valid for 1 hour

	tokenString, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		t.Fatalf("MakeJWT failed with error: %v", err) // Fail the test if MakeJWT errors
	}
	if tokenString == "" {
		t.Fatal("MakeJWT returned an empty token string") // Fail if it returns empty
	}

	validatedUserID, err := ValidateJWT(tokenString, tokenSecret)
	if err != nil {
		t.Fatalf("ValidateJWT failed with error: %v", err)
	}

	if validatedUserID != userID {
		t.Errorf("Validated User ID %s does not match original User ID %s", validatedUserID, userID)
	}

}

func TestMakeAndValidateJWT_ExpiredToken(t *testing.T) {
	userID := uuid.New() // A new UUID for our test user
	tokenSecret := "some-super-secret-key"
	expiresIn := 5 * time.Second // Token valid for 5 seconds

	tokenString, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		t.Fatalf("MakeJWT failed with error: %v", err) // Fail the test if MakeJWT errors
	}
	if tokenString == "" {
		t.Fatal("MakeJWT returned an empty token string") // Fail if it returns empty
	}

	time.Sleep(10 * time.Second) //wait 10 seconds to expire token

	_, err = ValidateJWT(tokenString, tokenSecret)
	if err == nil {
		t.Fatalf("ValidateJWT failed with error: %v", err)
	}
}

func TestMakeAndValidateJWT_WrongSecret(t *testing.T) {
	userID := uuid.New() // A new UUID for our test user
	tokenSecret := "some-super-secret-key"
	expiresIn := time.Hour // Token valid for 5 seconds
	wrongSecret := "totally-the-wrong-secret"

	tokenString, err := MakeJWT(userID, tokenSecret, expiresIn)
	if err != nil {
		t.Fatalf("MakeJWT failed with error: %v", err) // Fail the test if MakeJWT errors
	}
	if tokenString == "" {
		t.Fatal("MakeJWT returned an empty token string") // Fail if it returns empty
	}

	_, err = ValidateJWT(tokenString, wrongSecret)
	if err == nil {
		t.Fatalf("ValidateJWT failed with error: %v", err)
	}
}

func TestGetBearerToken(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer some_token_here"},
	}
	token := "some_token_here"
	bearerToken, err := GetBearerToken(headers)
	if bearerToken != token {
		t.Fatalf("Auth failed Expected %v, got %v", token, bearerToken)
	}
	if err != nil {
		t.Fatalf("Auth failed with error: %v", err)
	}
}

func TestGetBearerTokenWithSpaces(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"Bearer               some_token_here"},
	}
	token := "some_token_here"
	bearerToken, err := GetBearerToken(headers)
	if bearerToken != token {
		t.Fatalf("Auth failed Expected %v, got %v", token, bearerToken)
	}
	if err != nil {
		t.Fatalf("Auth failed with error: %v", err)
	}
}

func TestGetBearerToken_Blank(t *testing.T) {
	headers := http.Header{}
	token := ""
	bearerToken, err := GetBearerToken(headers)
	if bearerToken != token {
		t.Fatalf("Auth failed Expected %v, got %v", token, bearerToken)
	}
	if err == nil {
		t.Fatalf("Auth failed with error: %v", err)
	}
}

func TestGetBearerToken_NoBearer(t *testing.T) {
	headers := http.Header{
		"Authorization": []string{"    some_token_here"},
	}
	token := ""
	bearerToken, err := GetBearerToken(headers)
	if bearerToken != token {
		t.Fatalf("Auth failed Expected %v, got %v", token, bearerToken)
	}
	if err == nil {
		t.Fatalf("Auth failed with error: %v", err)
	}
}
