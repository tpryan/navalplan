package datastore

import (
	"app/models"
	"time"
)

type Store interface {
	// Voyages
	ListVoyages(personID int64) ([]models.Voyage, error)
	CreateVoyage(v *models.Voyage) error
	GetVoyage(id int64) (*models.Voyage, error)
	UpdateVoyage(v *models.Voyage) error
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

	// Voyage Guide
	GetVoyageGuide(voyageID int64) (*models.VoyageGuide, error)
	CreateVoyageGuide(g *models.VoyageGuide) error

	// Person
	FindPersonByGoogleID(googleID string) (*models.Person, error)
	GetPersonByID(id int64) (*models.Person, error)
	CreatePerson(googleID, email, name, pictureURL string) (*models.Person, error)
	UpdatePersonName(id int64, name string) error

	// Session
	CreateSession(token string, personID int64, expiresAt time.Time) error
	GetSession(token string) (*models.Session, error)
	DeleteSession(token string) error
}
