package storage

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kamuyin/kleinanzeigenFinder/config"
)

func TestDraftPersistenceAndLegacyMonitor(t *testing.T) {
	db, err := Connect(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := NewMonitorRepository(db)
	draft := config.MonitorConfig{Name: "flat", Criteria: strings.Repeat("Bright kitchen. ", 300), Rules: config.RulesConfig{IncludeAny: []string{"Balkon", "Terrasse"}, IncludeKeywords: []string{"Einbauk?che"}, ExcludeKeywords: []string{"Tausch"}, MinPrice: new(500.0), MaxPrice: new(1000.0)}}
	id, err := repo.SaveDraft(draft, "owner")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewMonitorRepository(db).GetDraft(id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(draft, *restored) {
		t.Fatal("draft data lost")
	}
	if _, err := repo.GetDraft(id, "someone-else"); err == nil {
		t.Fatal("draft owner ignored")
	}
	db.Model(&MonitorDraftModel{}).Where("id = ?", id).Update("expires_at", time.Now().Add(-time.Minute))
	if _, err := repo.GetDraft(id, "owner"); err == nil {
		t.Fatal("expired draft accepted")
	}
	if err := db.Create(&MonitorModel{Name: "legacy", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	old := repo.Get("legacy")
	if old == nil || old.Platform != "kleinanzeigen" || old.WatchPages != 2 {
		t.Fatalf("legacy monitor not migrated: %+v", old)
	}
	old.Platform = "future-platform"
	if err := repo.Upsert(*old, "test"); err == nil {
		t.Fatal("platform changed under existing deduplication key")
	}
}

func TestLegacyImmoScoutMonitorRetainsIdentity(t *testing.T) {
	db, err := Connect(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repo := NewMonitorRepository(db)
	if err := db.Create(&MonitorModel{Name: "old-immo", SearchURL: "https://www.immobilienscout24.de/Suche/de/berlin/berlin/wohnung-mieten", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	monitor := repo.Get("old-immo")
	if monitor == nil || monitor.Platform != "immoscout" {
		t.Fatalf("incorrect legacy routing %+v", monitor)
	}
	if err := repo.Upsert(*monitor, "test"); err != nil {
		t.Fatal(err)
	}
	var saved MonitorModel
	if err := db.First(&saved, "name = ?", "old-immo").Error; err != nil {
		t.Fatal(err)
	}
	if saved.Platform != "immoscout" {
		t.Fatal("provider identity was not persisted")
	}
}
