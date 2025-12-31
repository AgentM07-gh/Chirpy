-- name: GetUserUpdateInfo :one
SELECT email, updated_at, is_chirpy_red from users WHERE id = $1;