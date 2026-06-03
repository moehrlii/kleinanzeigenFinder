package storage

import (
	"encoding/json"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"gorm.io/gorm"
)

type ListingModel struct {
	Monitor     string `gorm:"primaryKey"`
	ListingID   string `gorm:"primaryKey"`
	URL         string
	Title       string
	FirstSeenAt time.Time
	NotifiedAt  *time.Time
	Accepted    bool
	Score       int
	Reason      string
	Payload     string
}

func (ListingModel) TableName() string {
	return "listings"
}

type SeenStore struct {
	db *gorm.DB
}

func NewSeenStore(db *gorm.DB) *SeenStore {
	_ = db.AutoMigrate(&ListingModel{})
	return &SeenStore{db: db}
}

func (s *SeenStore) HasSeen(monitor, listingID string) bool {
	var count int64
	s.db.Model(&ListingModel{}).Where("monitor = ? AND listing_id = ?", monitor, listingID).Count(&count)
	return count > 0
}

func (s *SeenStore) Save(monitor string, listing model.Listing, classification model.Classification, notified bool) error {
	payloadBytes, _ := json.Marshal(map[string]any{
		"listing":        listing,
		"classification": classification,
	})

	var notifiedAt *time.Time
	if notified {
		now := time.Now()
		notifiedAt = &now
	}

	modelRecord := ListingModel{
		Monitor:     monitor,
		ListingID:   listing.Identity(),
		URL:         listing.URL,
		Title:       listing.Title,
		FirstSeenAt: time.Now(),
		NotifiedAt:  notifiedAt,
		Accepted:    classification.Suitable,
		Score:       classification.Score,
		Reason:      classification.Reason,
		Payload:     string(payloadBytes),
	}

	var existing ListingModel
	result := s.db.Where("monitor = ? AND listing_id = ?", monitor, listing.Identity()).First(&existing)

	if result.Error == nil {
		modelRecord.FirstSeenAt = existing.FirstSeenAt // retain first seen time
	}

	return s.db.Save(&modelRecord).Error
}

func (s *SeenStore) Count(monitor string) int64 {
	var count int64
	query := s.db.Model(&ListingModel{})
	if monitor != "" {
		query = query.Where("monitor = ?", monitor)
	}
	query.Count(&count)
	return count
}
