package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
	"github.com/Kamuyin/kleinanzeigenFinder/model"
	"github.com/Kamuyin/kleinanzeigenFinder/providers"
	"gorm.io/gorm"
)

type MonitorModel struct {
	Platform                string
	Name                    string `gorm:"primaryKey"`
	SearchURL               string
	Criteria                string
	Mode                    string
	IntervalSeconds         int
	BackfillIntervalSeconds int
	WatchIntervalSeconds    int
	WatchPages              int
	MaxResults              int
	FetchDetails            bool
	MaxDetailPages          int
	Enabled                 bool
	RulesJSON               string
	Source                  string
	DiscordChannelID        *int64
	LastBackfillCompletedAt *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (MonitorModel) TableName() string {
	return "monitors"
}

type DiscordMessageModel struct {
	Monitor   string `gorm:"primaryKey"`
	ListingID string `gorm:"primaryKey"`
	ChannelID int64
	MessageID int64
	Accepted  bool
	CreatedAt time.Time
}

func (DiscordMessageModel) TableName() string {
	return "discord_messages"
}

type ListingFeedbackModel struct {
	Monitor   string `gorm:"primaryKey"`
	ListingID string `gorm:"primaryKey"`
	Status    string
	Note      *string
	UpdatedBy int64
	UpdatedAt time.Time
}

func (ListingFeedbackModel) TableName() string {
	return "listing_feedback"
}

type ListingQueueModel struct {
	Monitor       string `gorm:"primaryKey"`
	ListingID     string `gorm:"primaryKey"`
	URL           string
	Title         string
	Status        string `gorm:"index"`
	Attempts      int
	Payload       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastAttemptAt *time.Time
}

func (ListingQueueModel) TableName() string {
	return "listing_queue"
}

type MonitorRepository struct {
	db *gorm.DB
}

func NewMonitorRepository(db *gorm.DB) *MonitorRepository {
	_ = db.AutoMigrate(&MonitorModel{}, &DiscordMessageModel{}, &ListingFeedbackModel{}, &ListingQueueModel{})
	return &MonitorRepository{db: db}
}

func (r *MonitorRepository) List(includeDisabled bool) []config.MonitorConfig {
	var models []MonitorModel
	query := r.db.Model(&MonitorModel{})
	if !includeDisabled {
		query = query.Where("enabled = ?", true)
	}
	query.Order("name").Find(&models)

	var configs []config.MonitorConfig
	for _, m := range models {
		configs = append(configs, r.monitorFromModel(m))
	}
	return configs
}

func (r *MonitorRepository) Get(name string) *config.MonitorConfig {
	var m MonitorModel
	if err := r.db.Where("name = ?", name).First(&m).Error; err != nil {
		return nil
	}
	cfg := r.monitorFromModel(m)
	return &cfg
}

func (r *MonitorRepository) GetChannelID(name string) *int64 {
	var m MonitorModel
	if err := r.db.Select("discord_channel_id").Where("name = ?", name).First(&m).Error; err != nil {
		return nil
	}
	return m.DiscordChannelID
}

func (r *MonitorRepository) Upsert(monitor config.MonitorConfig, source string) error {
	rulesJSON, _ := json.Marshal(monitor.Rules)

	model := MonitorModel{
		Platform:                providers.MonitorPlatform(monitor.Platform, monitor.SearchURL),
		Name:                    monitor.Name,
		SearchURL:               monitor.SearchURL,
		Criteria:                monitor.Criteria,
		Mode:                    monitor.Mode,
		IntervalSeconds:         monitor.IntervalSeconds,
		BackfillIntervalSeconds: monitor.BackfillIntervalSeconds,
		WatchIntervalSeconds:    monitor.WatchIntervalSeconds,
		WatchPages:              monitor.WatchPages,
		MaxResults:              monitor.MaxResults,
		FetchDetails:            monitor.FetchDetails,
		MaxDetailPages:          monitor.MaxDetailPages,
		Enabled:                 monitor.Enabled,
		RulesJSON:               string(rulesJSON),
		Source:                  source,
		UpdatedAt:               time.Now(),
	}

	var existing MonitorModel
	if r.db.Where("name = ?", monitor.Name).First(&existing).Error == nil {
		if providers.MonitorPlatform(existing.Platform, existing.SearchURL) != providers.MonitorPlatform(monitor.Platform, monitor.SearchURL) {
			return fmt.Errorf("create a new monitor name when changing platforms")
		}
		model.CreatedAt = existing.CreatedAt
		model.DiscordChannelID = existing.DiscordChannelID
		model.LastBackfillCompletedAt = existing.LastBackfillCompletedAt
		if model.Mode == "" {
			model.Mode = existing.Mode
		}
	} else {
		model.CreatedAt = time.Now()
	}

	return r.db.Save(&model).Error
}

func (r *MonitorRepository) Delete(name string) bool {
	res := r.db.Where("name = ?", name).Delete(&MonitorModel{})
	return res.RowsAffected > 0
}

func (r *MonitorRepository) SetEnabled(name string, enabled bool) bool {
	res := r.db.Model(&MonitorModel{}).Where("name = ?", name).Updates(map[string]interface{}{
		"enabled":    enabled,
		"updated_at": time.Now(),
	})
	return res.RowsAffected > 0
}

func (r *MonitorRepository) SetChannelID(name string, channelID int64) error {
	return r.db.Model(&MonitorModel{}).Where("name = ?", name).Updates(map[string]interface{}{
		"discord_channel_id": channelID,
		"updated_at":         time.Now(),
	}).Error
}

func (r *MonitorRepository) SetMode(name, mode string) error {
	updates := map[string]interface{}{
		"mode":       mode,
		"updated_at": time.Now(),
	}
	if mode == "watch" {
		now := time.Now()
		updates["last_backfill_completed_at"] = &now
	}
	return r.db.Model(&MonitorModel{}).Where("name = ?", name).Updates(updates).Error
}

func (r *MonitorRepository) EnqueueListings(monitor string, listings []model.Listing) int {
	enqueued := 0
	for _, listing := range listings {
		listingID := listing.Identity()
		if listingID == "" {
			continue
		}

		var seenCount int64
		r.db.Model(&ListingModel{}).Where("monitor = ? AND listing_id = ?", monitor, listingID).Count(&seenCount)
		if seenCount > 0 {
			continue
		}

		var queueCount int64
		r.db.Model(&ListingQueueModel{}).Where("monitor = ? AND listing_id = ?", monitor, listingID).Count(&queueCount)
		if queueCount > 0 {
			continue
		}

		payload, _ := json.Marshal(listing)
		now := time.Now()
		err := r.db.Create(&ListingQueueModel{
			Monitor:   monitor,
			ListingID: listingID,
			URL:       listing.URL,
			Title:     listing.Title,
			Status:    "queued",
			Payload:   string(payload),
			CreatedAt: now,
			UpdatedAt: now,
		}).Error
		if err == nil {
			enqueued++
		}
	}
	return enqueued
}

func (r *MonitorRepository) ListQueued(monitor string, limit int) []model.Listing {
	var rows []ListingQueueModel
	query := r.db.Where("monitor = ? AND status IN ?", monitor, []string{"queued", "failed"}).Order("attempts ASC, created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	query.Find(&rows)

	result := make([]model.Listing, 0, len(rows))
	for _, row := range rows {
		var listing model.Listing
		if err := json.Unmarshal([]byte(row.Payload), &listing); err != nil {
			listing = model.Listing{
				ID:    row.ListingID,
				URL:   row.URL,
				Title: row.Title,
			}
		}
		if listing.ID == "" {
			listing.ID = row.ListingID
		}
		result = append(result, listing)
	}
	return result
}

func (r *MonitorRepository) MarkQueuedProcessed(monitor, listingID string) {
	r.updateQueueStatus(monitor, listingID, "processed", false)
}

func (r *MonitorRepository) MarkQueuedSkipped(monitor, listingID string) {
	r.updateQueueStatus(monitor, listingID, "skipped", false)
}

func (r *MonitorRepository) MarkQueuedFailed(monitor, listingID string) {
	r.updateQueueStatus(monitor, listingID, "failed", true)
}

func (r *MonitorRepository) OpenQueueCount(monitor string) int64 {
	var count int64
	r.db.Model(&ListingQueueModel{}).Where("monitor = ? AND status IN ?", monitor, []string{"queued", "failed"}).Count(&count)
	return count
}

func (r *MonitorRepository) updateQueueStatus(monitor, listingID, status string, incrementAttempts bool) {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if incrementAttempts {
		now := time.Now()
		updates["last_attempt_at"] = &now
		updates["attempts"] = gorm.Expr("attempts + ?", 1)
	}
	r.db.Model(&ListingQueueModel{}).Where("monitor = ? AND listing_id = ?", monitor, listingID).Updates(updates)
}

func (r *MonitorRepository) SaveDiscordMessage(monitor, listingID string, channelID, messageID int64, accepted bool) error {
	return r.db.Save(&DiscordMessageModel{
		Monitor:   monitor,
		ListingID: listingID,
		ChannelID: channelID,
		MessageID: messageID,
		Accepted:  accepted,
		CreatedAt: time.Now(),
	}).Error
}

func (r *MonitorRepository) GetDiscordMessage(monitor, listingID string) *DiscordMessageModel {
	var msg DiscordMessageModel
	if err := r.db.Where("monitor = ? AND listing_id = ?", monitor, listingID).First(&msg).Error; err != nil {
		return nil
	}
	return &msg
}

func (r *MonitorRepository) SetFeedback(monitor, listingID, status string, userID int64, note *string) error {
	record := ListingFeedbackModel{
		Monitor:   monitor,
		ListingID: listingID,
		Status:    status,
		Note:      note,
		UpdatedBy: userID,
		UpdatedAt: time.Now(),
	}

	var existing ListingFeedbackModel
	if r.db.Where("monitor = ? AND listing_id = ?", monitor, listingID).First(&existing).Error == nil {
		if note == nil && existing.Note != nil {
			record.Note = existing.Note // preserve note if not provided
		}
	}

	return r.db.Save(&record).Error
}

func (r *MonitorRepository) monitorFromModel(m MonitorModel) config.MonitorConfig {
	var rules config.RulesConfig
	_ = json.Unmarshal([]byte(m.RulesJSON), &rules)
	return config.MonitorConfig{
		Platform:                providers.MonitorPlatform(m.Platform, m.SearchURL),
		Name:                    m.Name,
		SearchURL:               m.SearchURL,
		Criteria:                m.Criteria,
		Mode:                    defaultString(m.Mode, "backfill"),
		IntervalSeconds:         defaultInt(m.IntervalSeconds, defaultInt(m.BackfillIntervalSeconds, 300)),
		BackfillIntervalSeconds: defaultInt(m.BackfillIntervalSeconds, defaultInt(m.IntervalSeconds, 300)),
		WatchIntervalSeconds:    defaultInt(m.WatchIntervalSeconds, 1800),
		WatchPages:              defaultInt(m.WatchPages, 2),
		MaxResults:              m.MaxResults,
		FetchDetails:            m.FetchDetails,
		MaxDetailPages:          defaultInt(m.MaxDetailPages, 8),
		Enabled:                 m.Enabled,
		Rules:                   rules,
	}
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
