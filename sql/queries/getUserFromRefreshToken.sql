-- name: GetUserFromRefreshToken :one
SELECT user_id from refresh_tokens 
WHERE token = $1 AND expires_at > NOW() AND revoked_at IS NULL;