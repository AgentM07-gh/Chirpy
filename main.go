package main

import (
	"Chirpy/internal/auth"
	"Chirpy/internal/database"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type errorResponse struct {
	Error string `json:"error"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	RefToken  string    `json:"refresh_token"`
	IsRed     bool      `json:"is_chirpy_red"`
}

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	secret         string
	polka_key      string
}

func main() {
	godotenv.Load()
	err := godotenv.Load()
	const filepathRoot = "."
	const port = "8080"
	currentDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting current working directory: %v", err)
	}
	fmt.Println("Current working directory:", currentDir)

	loadErr := godotenv.Load()
	if loadErr != nil {
		log.Fatalf("Error loading .env file: %v", loadErr)
	}

	dbURL := os.Getenv("DB_URL")
	JWL_SECRET := os.Getenv("API_SECRET")
	polka_key := os.Getenv("POLKA_KEY")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}

	dbQueries := database.New(db)

	platform := os.Getenv("PLATFORM")
	if platform == "" {
		log.Fatal("PLATFORM must be set")
	}

	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
		db:             dbQueries,
		platform:       platform,
		secret:         JWL_SECRET,
		polka_key:      polka_key,
	}

	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir(filepathRoot))
	strippedHandler := http.StripPrefix("/app", fileServer)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(strippedHandler))

	mux.HandleFunc("GET /api/healthz", healthzHandler)
	mux.HandleFunc("GET /admin/metrics", apiCfg.metricsHandler)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
	mux.HandleFunc("POST /api/users", apiCfg.handlerUsersCreate)
	mux.HandleFunc("POST /api/chirps", apiCfg.chirps)
	mux.HandleFunc("GET /api/chirps", apiCfg.getAllChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirp)
	mux.HandleFunc("POST /api/login", apiCfg.userLogin)
	mux.HandleFunc("POST /api/refresh", apiCfg.userRefreshToken)
	mux.HandleFunc("POST /api/revoke", apiCfg.userRevokeRefreshToken)
	mux.HandleFunc("PUT /api/users", apiCfg.userUpdate)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.deleteChirp)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.userUpdateToRed)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	log.Fatal(server.ListenAndServe())
}

//------------------------------------------------------------------------

func (cfg *apiConfig) userUpdateToRed(w http.ResponseWriter, r *http.Request) {
	type UserIdData struct {
		UserID string `json:"user_id"`
	}
	type PolkaEvent struct {
		Event string     `json:"event"`
		Data  UserIdData `json:"data"`
	}
	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
	}
	if apiKey != cfg.polka_key {
		w.WriteHeader(http.StatusUnauthorized)
	}

	decoder := json.NewDecoder(r.Body)
	polkaEvent := PolkaEvent{}
	err = decoder.Decode(&polkaEvent)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON Body")
		return
	}
	if polkaEvent.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	userUUID, err := uuid.Parse(polkaEvent.Data.UserID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid User UUID")
		return
	}
	_, err = cfg.db.UpdateUserToRed(r.Context(), userUUID)
	if errors.Is(err, sql.ErrNoRows) {
		respondWithError(w, http.StatusNotFound, "No user found")
		return
	} else if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}
	// If we get here, err == nil, so success
	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {

	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
	}

	userUUID, err := auth.ValidateJWT(bearerToken, cfg.secret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	chirpID := r.PathValue("chirpID")
	chirpUUID, err := uuid.Parse(chirpID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ChirpID")
		return
	}
	dbChirp, err := cfg.db.GetChirp(r.Context(), chirpUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "Could not find Chirp")
			return
		} else {
			respondWithError(w, http.StatusInternalServerError, "Something went wrong")
			return
		}
	}
	if dbChirp.UserID != userUUID {
		respondWithError(w, http.StatusForbidden, "Status Forbidden")
		return
	} else {
		err = cfg.db.DeleteChirp(r.Context(), chirpUUID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Something went wrong")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (cfg *apiConfig) userUpdate(w http.ResponseWriter, r *http.Request) {
	type UserCreds struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	bearerToken, err := auth.GetBearerToken(r.Header)

	userUUID, err := auth.ValidateJWT(bearerToken, cfg.secret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	decoder := json.NewDecoder(r.Body)
	user := UserCreds{}
	err = decoder.Decode(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON Body")
		return
	}
	_, err = mail.ParseAddress(user.Email)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Not a valide email eddress")
		return
	}
	hashedPassword, err := auth.HashPassword(user.Password)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could Not hash password")
		return
	}

	params := database.UpdateUserCredsParams{
		ID:             userUUID,
		Email:          user.Email,
		HashedPassword: hashedPassword,
	}
	err = cfg.db.UpdateUserCreds(r.Context(), params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not update user in database")
		return
	}
	dbUserUpdate, err := cfg.db.GetUserUpdateInfo(r.Context(), userUUID)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}
	type UserUpdateResponse struct {
		Email   string `json:"email"`
		Updated string `json:"updated"`
		IsRed   bool   `json:"is_chirp_red"`
	}
	httpUser := UserUpdateResponse{
		Email:   dbUserUpdate.Email,
		Updated: dbUserUpdate.UpdatedAt.GoString(),
		IsRed:   dbUserUpdate.IsChirpyRed,
	}

	respondWithJSON(w, http.StatusOK, httpUser)

}

func (cfg *apiConfig) userLogin(w http.ResponseWriter, r *http.Request) {
	type UserLogin struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		//		ExpiresIn *int   `json:"expires_in_seconds"`
	}
	decoder := json.NewDecoder(r.Body)
	loginUser := UserLogin{}
	err := decoder.Decode(&loginUser)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON Body")
		return
	}
	_, err = mail.ParseAddress(loginUser.Email)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Incorrect email or password")
		return
	}

	/*
		expires_in_seconds := 3600

		if loginUser.ExpiresIn != nil && *loginUser.ExpiresIn != 0 && *loginUser.ExpiresIn < 3600 {
			expires_in_seconds = *loginUser.ExpiresIn
		}
	*/

	dbUser, err := cfg.db.GetUserByEmail(r.Context(), loginUser.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}
	ok, err := auth.CheckPasswordHash(loginUser.Password, dbUser.HashedPassword)
	if err != nil || !ok {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}
	duration := time.Duration(3600) * time.Second
	userToken, err := auth.MakeJWT(dbUser.ID, cfg.secret, duration)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	params := database.CreateRefreshTokenParams{
		Token:  refreshToken,
		UserID: dbUser.ID,
	}

	_, err = cfg.db.CreateRefreshToken(r.Context(), params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not create RefreshToken")
		return
	}

	httpUser := User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
		Token:     userToken,
		RefToken:  refreshToken,
		IsRed:     dbUser.IsChirpyRed,
	}

	respondWithJSON(w, http.StatusOK, httpUser)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	chirpID := r.PathValue("chirpID")
	cID, err := uuid.Parse(chirpID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid UUID")
		return
	}
	dbChirp, err := cfg.db.GetChirp(r.Context(), cID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "Could not find Chirp")
			return
		} else {
			respondWithError(w, http.StatusInternalServerError, "Something went wrong")
			return
		}
	}
	respondWithJSON(w, http.StatusOK, dbChirp)
}

func (cfg *apiConfig) getAllChirps(w http.ResponseWriter, r *http.Request) {
	authID := r.URL.Query().Get("author_id")

	var dbChirps []database.Chirp
	var err error

	if authID == "" {
		dbChirps, err = cfg.db.GetAllChirps(r.Context())
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Something went wrong")
			return
		}
	} else {
		userID, err := uuid.Parse(authID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid User ID")
			return
		}
		dbChirps, err = cfg.db.GetAllChirpsFromUser(r.Context(), userID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Something went wrong")
			return
		}
	}
	sortType := r.URL.Query().Get("sort")
	if sortType == "desc" {
		sort.Slice(dbChirps, func(i, j int) bool {
			return dbChirps[i].CreatedAt.After(dbChirps[j].CreatedAt)
		})
	}

	// here you can use dbChirps once and respond
	respondWithJSON(w, http.StatusOK, dbChirps)
}

func (cfg *apiConfig) chirps(w http.ResponseWriter, r *http.Request) {

	type Chirp struct {
		Body string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	chirp := Chirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	userUUID, err := auth.ValidateJWT(bearerToken, cfg.secret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	/*()
	// Convert string UUID to uuid.UUID type
	userUUID, err := uuid.Parse(chirp.UserID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	if validatedUser != userUUID {
		respondWithError(w, http.StatusUnauthorized, "Invalid Token")
		return
	}
	*/
	if len(chirp.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	clean := badWordReplacement(chirp.Body)

	// Create the params using the generated struct
	params := database.CreateChirpParams{
		Body:   clean,
		UserID: userUUID,
	}

	createdChirp, err := cfg.db.CreateChirp(r.Context(), params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not create Chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, createdChirp)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	hits := cfg.fileserverHits.Load()
	message := fmt.Sprintf(`
	<html>
  		<body>
    		<h1>Welcome, Chirpy Admin</h1>
    		<p>Chirpy has been visited %d times!</p>
  		</body>
	</html>`, hits)
	w.Write([]byte(message))
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Platform not DEV")
		return
	}

	// delete all users via SQLC
	err := cfg.db.Reset(r.Context()) // name will depend on your generated code
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset users")
		return
	}

	cfg.fileserverHits.Store(0)
	w.WriteHeader(http.StatusOK)
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func badWordReplacement(inputChirp string) string {
	badWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Split(inputChirp, " ")
	for i, value := range words {
		if slices.Contains(badWords, strings.ToLower(value)) {
			words[i] = "****"
		}
	}
	cleanChirp := strings.Join(words, " ")
	return cleanChirp
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, errorResponse{Error: msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	data, err := json.Marshal(payload)
	if err != nil {
		// If marshalling fails, send a simple fallback
		http.Error(w, "could not marshal JSON", http.StatusInternalServerError)
		return
	}

	w.Write(data)
}

func (cfg *apiConfig) handlerUsersCreate(w http.ResponseWriter, r *http.Request) {
	type newUser struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	nuser := newUser{}
	err := decoder.Decode(&nuser)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON Body")
		return
	}
	_, eerr := mail.ParseAddress(nuser.Email)
	if eerr != nil {
		respondWithError(w, http.StatusBadRequest, "Not a valide email eddress")
		return
	}
	hashedPassword, err := auth.HashPassword(nuser.Password)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could Not hash password")
		return
	}

	params := database.CreateUserParams{
		Email:          nuser.Email,
		HashedPassword: hashedPassword,
	}

	dbUser, err := cfg.db.CreateUser(r.Context(), params)
	if err != nil {
		log.Println("CreateUser error:", err) // <-- add this
		respondWithError(w, http.StatusInternalServerError, "Could not Create user and add to database")
		return
	}
	httpUser := User{
		ID:        dbUser.ID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
		Email:     dbUser.Email,
		IsRed:     dbUser.IsChirpyRed,
	}
	respondWithJSON(w, http.StatusCreated, httpUser)
}

func (cfg *apiConfig) userRefreshToken(w http.ResponseWriter, r *http.Request) {
	type accessToken struct {
		Token string `json:"token"`
	}
	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	userID, err := cfg.db.GetUserFromRefreshToken(r.Context(), bearerToken)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	duration := time.Duration(3600) * time.Second
	newAccessToken, err := auth.MakeJWT(userID, cfg.secret, duration)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	userJWT := accessToken{
		Token: newAccessToken,
	}
	respondWithJSON(w, http.StatusOK, userJWT)
}

func (cfg *apiConfig) userRevokeRefreshToken(w http.ResponseWriter, r *http.Request) {
	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}
	err = cfg.db.RevokeRefreshToken(r.Context(), bearerToken)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
