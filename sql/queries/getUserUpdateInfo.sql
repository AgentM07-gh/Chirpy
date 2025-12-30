-- name: GetUserUpdateInfo :one
SELECT email, updated_at from users WHERE id = $1;