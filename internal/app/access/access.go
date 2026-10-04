// Package access is who is asking: the signed-in user carried on a request's
// context and the checks that a Bot (or one of its rows) belongs to them. The
// domain packages under internal/app share it so none of them reaches back into
// package app for an ownership check.
package access

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

type userKey struct{}

// WithUser returns ctx carrying u as the signed-in user.
func WithUser(ctx context.Context, u *db.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// User is the signed-in user on ctx, or nil.
func User(ctx context.Context) *db.User {
	u, _ := ctx.Value(userKey{}).(*db.User)
	return u
}

// RequireAdmin is PermissionDenied unless the signed-in user is an admin.
func RequireAdmin(ctx context.Context) error {
	u := User(ctx)
	if u == nil || !u.Admin {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin only"))
	}
	return nil
}

// OwnBot loads the Bot with this id if it belongs to the signed-in user;
// anything else is NotFound, so one user cannot probe another's ids.
func OwnBot(ctx context.Context, gdb *gorm.DB, id string) (*db.Bot, error) {
	u := User(ctx)
	var b db.Bot
	if err := gdb.First(&b, "id = ? AND user_id = ?", id, u.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, err
	}
	return &b, nil
}

// BotRow loads one row of a Bot's by its id; what names the row in the
// not-found error. The caller has already established that the Bot is theirs.
func BotRow[T any](gdb *gorm.DB, botID, id, what string) (*T, error) {
	var row T
	res := gdb.Where("bot_id = ? AND id = ?", botID, id).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New(what+" not found"))
	}
	return &row, nil
}

// OwnBotRow is BotRow for the signed-in user's own Bot.
func OwnBotRow[T any](ctx context.Context, gdb *gorm.DB, botID, id, what string) (*T, error) {
	if _, err := OwnBot(ctx, gdb, botID); err != nil {
		return nil, err
	}
	return BotRow[T](gdb, botID, id, what)
}

// RowByToken resolves an Authorization header to the row whose hashColumn holds
// the hash of its bearer token, and returns the raw token so the caller can mask
// it. A missing or unknown token is Unauthenticated; a store failure is itself.
func RowByToken[T any](gdb *gorm.DB, hashColumn, header string) (*T, string, error) {
	tok := strings.TrimPrefix(header, "Bearer ")
	if tok == "" || gdb == nil {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	var row T
	res := gdb.Where(hashColumn+" = ?", ids.Hash(tok)).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, "", res.Error
	}
	if res.RowsAffected == 0 {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	return &row, tok, nil
}

// HTTPSessionError is sessionError for plain HTTP handlers.
func HTTPSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrAuth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	log.Printf("session lookup: %v", err)
	http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
}

type httpKey int

const (
	reqKey httpKey = iota
	rwKey
)

// WithHTTP makes the request and its response writer reachable from the
// context of the RPCs served through next (the sign-in cookie, the public URL).
func WithHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), reqKey, r)
		ctx = context.WithValue(ctx, rwKey, w)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Request is the HTTP request an RPC arrived on, or nil.
func Request(ctx context.Context) *http.Request {
	r, _ := ctx.Value(reqKey).(*http.Request)
	return r
}

// ResponseWriter is the HTTP response writer an RPC will answer on, or nil.
func ResponseWriter(ctx context.Context) http.ResponseWriter {
	w, _ := ctx.Value(rwKey).(http.ResponseWriter)
	return w
}
