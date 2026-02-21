//go:build giveaway

package database

import (
	"os"
	"testing"
	"time"

	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

func setupTestGiveawayDB(t *testing.T) *GiveawayDB {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "giveaway-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	db, err := NewGiveaway(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewGiveaway: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func TestGiveawayDB_CreateAndGetItem(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID:           "item-001",
		Title:        "Standing Desk",
		Description:  "Adjustable standing desk, works great",
		ImageURL:     "/static/images/desk.jpg",
		Condition:    models.ConditionGood,
		Status:       models.ItemStatusAvailable,
		DistMiles:    5.0,
		DriveMinutes: 15,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	got, err := db.GetItem("item-001")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got == nil {
		t.Fatal("GetItem returned nil")
	}
	if got.Title != "Standing Desk" {
		t.Errorf("Title = %q, want %q", got.Title, "Standing Desk")
	}
	if got.DistMiles != 5.0 {
		t.Errorf("DistMiles = %v, want 5.0", got.DistMiles)
	}
	if got.DriveMinutes != 15 {
		t.Errorf("DriveMinutes = %v, want 15", got.DriveMinutes)
	}
	if got.Status != models.ItemStatusAvailable {
		t.Errorf("Status = %v, want %v", got.Status, models.ItemStatusAvailable)
	}
}

func TestGiveawayDB_GetItem_NotFound(t *testing.T) {
	db := setupTestGiveawayDB(t)

	got, err := db.GetItem("nonexistent")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent item, got %+v", got)
	}
}

func TestGiveawayDB_ListItems(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	items := []models.Item{
		{ID: "item-a", Title: "Chair", Status: models.ItemStatusAvailable, Condition: models.ConditionGood, CreatedAt: now, UpdatedAt: now},
		{ID: "item-b", Title: "Table", Status: models.ItemStatusAvailable, Condition: models.ConditionFair, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "item-c", Title: "Lamp", Status: models.ItemStatusGone, Condition: models.ConditionGood, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
	}
	for i := range items {
		if err := db.CreateItem(&items[i]); err != nil {
			t.Fatalf("CreateItem %s: %v", items[i].ID, err)
		}
	}

	// All items
	all, err := db.ListItems("")
	if err != nil {
		t.Fatalf("ListItems (all): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListItems (all) len = %d, want 3", len(all))
	}

	// Available only
	avail, err := db.ListItems(models.ItemStatusAvailable)
	if err != nil {
		t.Fatalf("ListItems (available): %v", err)
	}
	if len(avail) != 2 {
		t.Errorf("ListItems (available) len = %d, want 2", len(avail))
	}

	// Gone only
	gone, err := db.ListItems(models.ItemStatusGone)
	if err != nil {
		t.Fatalf("ListItems (gone): %v", err)
	}
	if len(gone) != 1 {
		t.Errorf("ListItems (gone) len = %d, want 1", len(gone))
	}
}

func TestGiveawayDB_UpdateItem(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-upd", Title: "Old Title", Status: models.ItemStatusAvailable,
		Condition: models.ConditionGood, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	item.Title = "New Title"
	item.Status = models.ItemStatusClaimed
	if err := db.UpdateItem(item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	got, err := db.GetItem("item-upd")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.Title != "New Title" {
		t.Errorf("Title = %q, want %q", got.Title, "New Title")
	}
	if got.Status != models.ItemStatusClaimed {
		t.Errorf("Status = %v, want %v", got.Status, models.ItemStatusClaimed)
	}
}

func TestGiveawayDB_DeleteItem(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-del", Title: "Delete Me", Status: models.ItemStatusAvailable,
		Condition: models.ConditionPoor, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	if err := db.DeleteItem("item-del"); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	got, err := db.GetItem("item-del")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestGiveawayDB_CreateAndGetClaim(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-claim", Title: "Bookshelf", Status: models.ItemStatusAvailable,
		Condition: models.ConditionGood, DistMiles: 3, DriveMinutes: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	claim := &models.Claim{
		ID:           "claim-001",
		ItemID:       "item-claim",
		ClaimerName:  "Jane Doe",
		ClaimerEmail: "jane@example.com",
		ClaimerPhone: "555-1234",
		DeliveryFee:  8.17,
		Status:       models.ClaimStatusPending,
		Notes:        "Available after 6pm",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.CreateClaim(claim); err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	got, err := db.GetClaim("claim-001")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got == nil {
		t.Fatal("GetClaim returned nil")
	}
	if got.ClaimerName != "Jane Doe" {
		t.Errorf("ClaimerName = %q, want %q", got.ClaimerName, "Jane Doe")
	}
	if got.DeliveryFee != 8.17 {
		t.Errorf("DeliveryFee = %v, want 8.17", got.DeliveryFee)
	}
	if got.Status != models.ClaimStatusPending {
		t.Errorf("Status = %v, want %v", got.Status, models.ClaimStatusPending)
	}
}

func TestGiveawayDB_ListClaimsByItem(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-multi", Title: "TV", Status: models.ItemStatusAvailable,
		Condition: models.ConditionGood, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	claims := []models.Claim{
		{ID: "c1", ItemID: "item-multi", ClaimerName: "Alice", ClaimerEmail: "a@b.com", Status: models.ClaimStatusPending, CreatedAt: now, UpdatedAt: now},
		{ID: "c2", ItemID: "item-multi", ClaimerName: "Bob", ClaimerEmail: "b@b.com", Status: models.ClaimStatusCancelled, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	}
	for i := range claims {
		if err := db.CreateClaim(&claims[i]); err != nil {
			t.Fatalf("CreateClaim %s: %v", claims[i].ID, err)
		}
	}

	got, err := db.ListClaimsByItem("item-multi")
	if err != nil {
		t.Fatalf("ListClaimsByItem: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestGiveawayDB_UpdateClaimStatus(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-cs", Title: "Couch", Status: models.ItemStatusAvailable,
		Condition: models.ConditionFair, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	claim := &models.Claim{
		ID: "claim-cs", ItemID: "item-cs", ClaimerName: "Test",
		ClaimerEmail: "t@t.com", Status: models.ClaimStatusPending,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateClaim(claim); err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	if err := db.UpdateClaimStatus("claim-cs", models.ClaimStatusConfirmed); err != nil {
		t.Fatalf("UpdateClaimStatus: %v", err)
	}

	got, err := db.GetClaim("claim-cs")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Status != models.ClaimStatusConfirmed {
		t.Errorf("Status = %v, want %v", got.Status, models.ClaimStatusConfirmed)
	}
}

func TestGiveawayDB_ListClaims(t *testing.T) {
	db := setupTestGiveawayDB(t)
	now := time.Now().Truncate(time.Second)

	item := &models.Item{
		ID: "item-lc", Title: "Desk", Status: models.ItemStatusAvailable,
		Condition: models.ConditionGood, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateItem(item); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	claims := []models.Claim{
		{ID: "lc1", ItemID: "item-lc", ClaimerName: "A", ClaimerEmail: "a@a.com", Status: models.ClaimStatusPending, CreatedAt: now, UpdatedAt: now},
		{ID: "lc2", ItemID: "item-lc", ClaimerName: "B", ClaimerEmail: "b@b.com", Status: models.ClaimStatusConfirmed, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	}
	for i := range claims {
		if err := db.CreateClaim(&claims[i]); err != nil {
			t.Fatalf("CreateClaim: %v", err)
		}
	}

	all, err := db.ListClaims("")
	if err != nil {
		t.Fatalf("ListClaims (all): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("len (all) = %d, want 2", len(all))
	}

	pending, err := db.ListClaims(models.ClaimStatusPending)
	if err != nil {
		t.Fatalf("ListClaims (pending): %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("len (pending) = %d, want 1", len(pending))
	}
}
