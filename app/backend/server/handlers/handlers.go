package handlers

import (
	"app/datastore"
)

type Handler struct {
	DB datastore.Store
}

func New(db datastore.Store) *Handler {
	return &Handler{DB: db}
}
