package datastore

import "app/models"

type Store interface {
	// Voyages
	ListVoyages(userID int64) ([]models.Voyage, error)
	CreateVoyage(v *models.Voyage) error
	GetVoyage(id int64) (*models.Voyage, error)
	UpdateVoyageSharing(id int64, shareToken *string, isPublic bool) error
	GetVoyageByToken(token string) (*models.Voyage, error)
	DeleteVoyage(id int64) error

	// Stops
	ListStops(voyageID int64) ([]models.Stop, error)
	CreateStop(s *models.Stop) error
	GetStop(id int64) (*models.Stop, error)
	UpdateStop(s *models.Stop) error
	DeleteStop(id int64) error

	// Briefings
	GetBriefing(stopID int64) (*models.Briefing, error)
	CreateBriefing(b *models.Briefing) error
}
