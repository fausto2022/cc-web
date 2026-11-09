package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type AuthUser struct {
	ID                 string
	Username           string
	PasswordHash       string
	MustChangePassword bool
}

type AuthSession struct {
	UserID             string
	Username           string
	CSRFToken          string
	ExpiresAt          time.Time
	MustChangePassword bool
}

func (s *Store) HasAuthUser(ctx context.Context) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM users LIMIT 1").Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) CreateAuthUser(ctx context.Context, username, passwordHash string) error {
	now := stamp(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,must_change_password,created_at,password_changed_at)
		SELECT 'admin',?,?,1,?,? WHERE NOT EXISTS(SELECT 1 FROM users)`, username, passwordHash, now, now)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (s *Store) AuthUserByUsername(ctx context.Context, username string) (AuthUser, error) {
	var user AuthUser
	var mustChange int
	err := s.db.QueryRowContext(ctx, "SELECT id,username,password_hash,must_change_password FROM users WHERE username=?", username).Scan(&user.ID, &user.Username, &user.PasswordHash, &mustChange)
	user.MustChangePassword = mustChange != 0
	if errors.Is(err, sql.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return user, err
}

func (s *Store) AuthUserByID(ctx context.Context, id string) (AuthUser, error) {
	var user AuthUser
	var mustChange int
	err := s.db.QueryRowContext(ctx, "SELECT id,username,password_hash,must_change_password FROM users WHERE id=?", id).Scan(&user.ID, &user.Username, &user.PasswordHash, &mustChange)
	user.MustChangePassword = mustChange != 0
	if errors.Is(err, sql.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return user, err
}

func (s *Store) CreateAuthSession(ctx context.Context, userID, tokenHash, csrfToken string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO auth_sessions(token_hash,user_id,csrf_token,created_at,expires_at) VALUES(?,?,?,?,?)", tokenHash, userID, csrfToken, stamp(time.Now()), stamp(expiresAt))
	return err
}

func (s *Store) AuthSession(ctx context.Context, tokenHash string, now time.Time) (AuthSession, error) {
	var session AuthSession
	var expires string
	var mustChange int
	err := s.db.QueryRowContext(ctx, `SELECT a.user_id,u.username,a.csrf_token,a.expires_at,u.must_change_password
		FROM auth_sessions a JOIN users u ON u.id=a.user_id WHERE a.token_hash=?`, tokenHash).Scan(&session.UserID, &session.Username, &session.CSRFToken, &expires, &mustChange)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthSession{}, ErrNotFound
	}
	if err != nil {
		return AuthSession{}, err
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return AuthSession{}, err
	}
	if !session.ExpiresAt.After(now) {
		return AuthSession{}, ErrNotFound
	}
	session.MustChangePassword = mustChange != 0
	return session, nil
}

func (s *Store) RevokeAuthSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE token_hash=?", tokenHash)
	return err
}

func (s *Store) ChangeAuthPassword(ctx context.Context, userID, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=?,must_change_password=0,password_changed_at=? WHERE id=?", passwordHash, stamp(time.Now()), userID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM auth_sessions WHERE user_id=?", userID); err != nil {
		return err
	}
	return tx.Commit()
}
