package kite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ajaynegi33/AlgoTrade/internal/config"
	"github.com/ajaynegi33/AlgoTrade/internal/database"
)

func TestKiteService_NotConfigured(testingInstance *testing.T) {
	tempDir := testingInstance.TempDir()
	db, err := database.Init(filepath.Join(tempDir, "test.db"))
	if err != nil {
		testingInstance.Fatalf("Failed to init db: %v", err)
	}
	defer database.Close()

	cfg := &config.Config{
		KiteAPIKey:    "",
		KiteAPISecret: "",
	}

	service := NewService(cfg, db)
	status := service.GetStatus()
	if status.IsConfigured {
		testingInstance.Errorf("Expected IsConfigured to be false")
	}
	if status.IsConnected {
		testingInstance.Errorf("Expected IsConnected to be false")
	}

	_, err = service.GetLoginURL()
	if err != ErrKiteNotConfigured {
		testingInstance.Errorf("Expected ErrKiteNotConfigured, got %v", err)
	}
}

func TestIsSessionExpired(testingInstance *testing.T) {
	istLoc := time.FixedZone("IST", 5*3600+1800)
	nowIST := time.Now().In(istLoc)

	// A session created 1 minute ago should NOT be expired
	freshSession := nowIST.Add(-1 * time.Minute)
	if isSessionExpired(freshSession) {
		testingInstance.Errorf("Fresh session should not be expired")
	}

	// A session created 48 hours ago MUST be expired
	oldSession := nowIST.Add(-48 * time.Hour)
	if !isSessionExpired(oldSession) {
		testingInstance.Errorf("48h old session must be expired")
	}
}
