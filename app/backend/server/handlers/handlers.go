package handlers

import (
	"app/datastore"
)

type Handler struct {
	DB *datastore.DB
}

func New(db *datastore.DB) *Handler {
	return &Handler{DB: db}
}
