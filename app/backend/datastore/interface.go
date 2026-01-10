package datastore

import (
	"context"
	"time"

	"app/models"
)

type Store interface {
	// Voyages
	ListVoyages(ctx context.Context, personID int64, limit, offset int) ([]models.Voyage, error)
	CreateVoyage(ctx context.Context, v *models.Voyage) error
	GetVoyage(ctx context.Context, id int64) (*models.Voyage, error)
	UpdateVoyage(ctx context.Context, v *models.Voyage) error
	UpdateVoyageSharing(ctx context.Context, id int64, shareToken *string, isPublic bool) error
	GetVoyageByToken(ctx context.Context, token string) (*models.Voyage, error)
	DeleteVoyage(ctx context.Context, id int64) error

	// Stops
	ListStops(ctx context.Context, voyageID int64, limit, offset int) ([]models.Stop, error)
	CreateStop(ctx context.Context, s *models.Stop) error
	GetStop(ctx context.Context, id int64) (*models.Stop, error)
	UpdateStop(ctx context.Context, s *models.Stop) error
	DeleteStop(ctx context.Context, id int64) error

	// Briefings
	GetBriefing(ctx context.Context, stopID int64) (*models.Briefing, error)
	ListVoyageBriefings(ctx context.Context, voyageID int64) ([]models.Briefing, error)
	CreateBriefing(ctx context.Context, b *models.Briefing) error

	// Voyage Guide
	GetVoyageGuide(ctx context.Context, voyageID int64) (*models.VoyageGuide, error)
	CreateVoyageGuide(ctx context.Context, g *models.VoyageGuide) error
	SaveVoyageMap(ctx context.Context, voyageID int64, data []byte) error
	GetVoyageMap(ctx context.Context, voyageID int64) ([]byte, error)

	// Person
	FindPersonByGoogleID(ctx context.Context, googleID string) (*models.Person, error)
	GetPersonByID(ctx context.Context, id int64) (*models.Person, error)
	CreatePerson(ctx context.Context, googleID, email, name string, pictureURL *string, invitedBy *int64) (*models.Person, error)
	UpdatePersonName(ctx context.Context, id int64, name string) error
	ListPeople(ctx context.Context) ([]models.Person, error)

	// Invitations
	GetInvitation(ctx context.Context, email string) (*models.Invitation, error)
	CreateInvitation(ctx context.Context, email string, invitedBy int64) error
	DeleteInvitation(ctx context.Context, email string) error
	ListInvitations(ctx context.Context) ([]models.Invitation, error)

	// Session
	CreateSession(ctx context.Context, token string, personID int64, expiresAt time.Time) error
	GetSession(ctx context.Context, token string) (*models.Session, error)
	DeleteSession(ctx context.Context, token string) error

	// Discovery
	ListRegionsByMonth(ctx context.Context, month int) ([]models.RegionWithSeasonality, error)
	GetRegionDetails(ctx context.Context, regionID int, month int) (*models.SailingRegion, *models.RegionSeasonality, error)
	UpsertRegion(ctx context.Context, region *models.SailingRegion) error
	UpsertSeasonality(ctx context.Context, seasonality *models.RegionSeasonality) error
	DeleteSeasonalityForMonth(ctx context.Context, month int) error
	GetAllRegions(ctx context.Context) ([]models.SailingRegion, error)
	DeleteSeasonality(ctx context.Context, regionID int, month int) error
}
