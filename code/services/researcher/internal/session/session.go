// Package session provides small decorators around ADK's session.Service.
package session

import (
	"context"
	"log/slog"

	"google.golang.org/adk/v2/session"
)

// AutoCreate wraps a session.Service so that Get transparently creates the
// session if it doesn't exist yet, instead of requiring callers to make a
// separate Create call before first use.
type AutoCreate struct {
	session.Service
}

// NewAutoCreate wraps inner with auto-create-on-Get behavior.
func NewAutoCreate(inner session.Service) *AutoCreate {
	return &AutoCreate{Service: inner}
}

// Get returns the session, creating it first if it does not already exist.
func (s *AutoCreate) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	resp, err := s.Service.Get(ctx, req)
	if err != nil {
		slog.Debug("Session not found, auto-creating", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)
		createResp, createErr := s.Service.Create(ctx, &session.CreateRequest{
			AppName:   req.AppName,
			UserID:    req.UserID,
			SessionID: req.SessionID,
		})
		if createErr != nil {
			if resp2, err2 := s.Service.Get(ctx, req); err2 == nil {
				return resp2, nil
			}
			return nil, createErr
		}
		return &session.GetResponse{Session: createResp.Session}, nil
	}
	return resp, nil
}
