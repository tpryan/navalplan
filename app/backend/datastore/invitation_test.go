package datastore

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetInvitation(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	email := "test@example.com"
	invitedBy := int64(1)
	createdAt := time.Now()

	rows := sqlmock.NewRows([]string{"email", "invited_by", "created_at"}).
		AddRow(email, invitedBy, createdAt)

	query := `SELECT * FROM invitation WHERE email = $1`

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(email).
		WillReturnRows(rows)

	invitation, err := db.GetInvitation(context.Background(), email)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invitation.Email != email {
		t.Errorf("expected email %s, got %s", email, invitation.Email)
	}
	if *invitation.InvitedBy != invitedBy {
		t.Errorf("expected invited_by %d, got %d", invitedBy, *invitation.InvitedBy)
	}

	// Test Not Found
	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs("nonexistent@example.com").
		WillReturnError(sql.ErrNoRows)

	_, err = db.GetInvitation(context.Background(), "nonexistent@example.com")
	if err != ErrInvitationNotFound {
		t.Errorf("expected ErrInvitationNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestCreateInvitation(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	email := "test@example.com"
	invitedBy := int64(1)

	query := `INSERT INTO invitation (email, invited_by) VALUES ($1, $2)`

	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(email, invitedBy).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.CreateInvitation(context.Background(), email, invitedBy)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestDeleteInvitation(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	email := "test@example.com"

	query := `DELETE FROM invitation WHERE email = $1`

	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs(email).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := db.DeleteInvitation(context.Background(), email)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestListInvitations(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"email", "invited_by", "created_at"}).
		AddRow("user1@example.com", 1, time.Now()).
		AddRow("user2@example.com", 2, time.Now())

	query := `SELECT * FROM invitation ORDER BY created_at DESC`

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(rows)

	invitations, err := db.ListInvitations(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(invitations) != 2 {
		t.Errorf("expected 2 invitations, got %d", len(invitations))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestListPeople(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	limit := 10
	offset := 0
	
	rows := sqlmock.NewRows([]string{"id", "google_id", "email", "name", "picture_url", "is_admin", "created_at"}).
		AddRow(1, "gid1", "p1@example.com", "Person 1", nil, false, time.Now()).
		AddRow(2, "gid2", "p2@example.com", "Person 2", nil, true, time.Now())

	query := `SELECT * FROM person ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WithArgs(limit, offset).
		WillReturnRows(rows)

	people, err := db.ListPeople(context.Background(), limit, offset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(people) != 2 {
		t.Errorf("expected 2 people, got %d", len(people))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestCountPeople(t *testing.T) {
	db, mock := mockDB(t)
	defer db.Close()

	expectedCount := 5
	rows := sqlmock.NewRows([]string{"count"}).AddRow(expectedCount)

	query := `SELECT COUNT(*) FROM person`

	mock.ExpectQuery(regexp.QuoteMeta(query)).
		WillReturnRows(rows)

	count, err := db.CountPeople(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count != expectedCount {
		t.Errorf("expected count %d, got %d", expectedCount, count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
