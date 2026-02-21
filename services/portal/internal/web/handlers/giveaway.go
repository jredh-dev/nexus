//go:build giveaway

package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jredh-dev/nexus/services/portal/pkg/fees"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// GiveawayList renders the public giveaway browse page (available items only).
func (h *Handler) GiveawayList(w http.ResponseWriter, r *http.Request) {
	items, err := h.giveawayDB.ListItems(models.ItemStatusAvailable)
	if err != nil {
		log.Printf("Error listing giveaway items: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Pre-calculate delivery fees for display.
	type itemWithFee struct {
		models.Item
		Fee fees.DeliveryFee
	}
	var display []itemWithFee
	for _, item := range items {
		fee := fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes)
		display = append(display, itemWithFee{Item: item, Fee: fee})
	}

	h.renderTemplate(w, "giveaway.html", map[string]interface{}{
		"Title":    "Free Stuff",
		"Year":     time.Now().Year(),
		"LoggedIn": h.isLoggedIn(r),
		"Items":    display,
	})
}

// GiveawayItem renders the detail page for a single item with claim form.
func (h *Handler) GiveawayItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.giveawayDB.GetItem(id)
	if err != nil {
		log.Printf("Error getting giveaway item %s: %v", id, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if item == nil {
		http.NotFound(w, r)
		return
	}

	fee := fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes)

	h.renderTemplate(w, "giveaway_item.html", map[string]interface{}{
		"Title":    item.Title,
		"Year":     time.Now().Year(),
		"LoggedIn": h.isLoggedIn(r),
		"Item":     item,
		"Fee":      fee,
	})
}

// GiveawayClaimSubmit handles the public claim form submission.
func (h *Handler) GiveawayClaimSubmit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.giveawayDB.GetItem(id)
	if err != nil || item == nil {
		http.NotFound(w, r)
		return
	}

	if item.Status != models.ItemStatusAvailable {
		h.renderTemplate(w, "giveaway_item.html", map[string]interface{}{
			"Title":    item.Title,
			"Year":     time.Now().Year(),
			"LoggedIn": h.isLoggedIn(r),
			"Item":     item,
			"Fee":      fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes),
			"Error":    "This item is no longer available.",
		})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	phone := r.FormValue("phone")

	if name == "" || email == "" {
		fee := fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes)
		h.renderTemplate(w, "giveaway_item.html", map[string]interface{}{
			"Title":    item.Title,
			"Year":     time.Now().Year(),
			"LoggedIn": h.isLoggedIn(r),
			"Item":     item,
			"Fee":      fee,
			"Error":    "Name and email are required.",
			"Name":     name,
			"Email":    email,
			"Phone":    phone,
		})
		return
	}

	fee := fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes)
	now := time.Now()

	claim := &models.Claim{
		ID:           generateID(),
		ItemID:       item.ID,
		ClaimerName:  name,
		ClaimerEmail: email,
		ClaimerPhone: phone,
		DeliveryFee:  fee.Total,
		Status:       models.ClaimStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.giveawayDB.CreateClaim(claim); err != nil {
		log.Printf("Error creating claim for item %s: %v", id, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Mark item as claimed.
	item.Status = models.ItemStatusClaimed
	if err := h.giveawayDB.UpdateItem(item); err != nil {
		log.Printf("Error updating item status for %s: %v", id, err)
	}

	h.renderTemplate(w, "giveaway_item.html", map[string]interface{}{
		"Title":    item.Title,
		"Year":     time.Now().Year(),
		"LoggedIn": h.isLoggedIn(r),
		"Item":     item,
		"Fee":      fee,
		"Success":  "Claim submitted! We'll be in touch about delivery.",
	})
}

// --- JSON API endpoints ---

// APIListItems returns available items as JSON.
func (h *Handler) APIListItems(w http.ResponseWriter, r *http.Request) {
	status := models.ItemStatus(r.URL.Query().Get("status"))
	if status == "" {
		status = models.ItemStatusAvailable
	}

	items, err := h.giveawayDB.ListItems(status)
	if err != nil {
		jsonError(w, "Failed to list items", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []models.Item{}
	}
	jsonResponse(w, items)
}

// APICalculateFee returns a delivery fee calculation as JSON.
func (h *Handler) APICalculateFee(w http.ResponseWriter, r *http.Request) {
	milesStr := r.URL.Query().Get("miles")
	minutesStr := r.URL.Query().Get("minutes")

	miles, err := strconv.ParseFloat(milesStr, 64)
	if err != nil {
		jsonError(w, "Invalid miles parameter", http.StatusBadRequest)
		return
	}

	minutes, err := strconv.Atoi(minutesStr)
	if err != nil {
		jsonError(w, "Invalid minutes parameter", http.StatusBadRequest)
		return
	}

	fee := fees.CalculateDeliveryDefault(miles, minutes)
	jsonResponse(w, fee)
}

// APICreateClaim handles a JSON claim submission.
func (h *Handler) APICreateClaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemID string `json:"item_id"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Phone  string `json:"phone"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ItemID == "" || req.Name == "" || req.Email == "" {
		jsonError(w, "item_id, name, and email are required", http.StatusBadRequest)
		return
	}

	item, err := h.giveawayDB.GetItem(req.ItemID)
	if err != nil || item == nil {
		jsonError(w, "Item not found", http.StatusNotFound)
		return
	}
	if item.Status != models.ItemStatusAvailable {
		jsonError(w, "Item is no longer available", http.StatusConflict)
		return
	}

	fee := fees.CalculateDeliveryDefault(item.DistMiles, item.DriveMinutes)
	now := time.Now()

	claim := &models.Claim{
		ID:           generateID(),
		ItemID:       item.ID,
		ClaimerName:  req.Name,
		ClaimerEmail: req.Email,
		ClaimerPhone: req.Phone,
		DeliveryFee:  fee.Total,
		Status:       models.ClaimStatusPending,
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.giveawayDB.CreateClaim(claim); err != nil {
		log.Printf("API: error creating claim for item %s: %v", req.ItemID, err)
		jsonError(w, "Failed to create claim", http.StatusInternalServerError)
		return
	}

	item.Status = models.ItemStatusClaimed
	if err := h.giveawayDB.UpdateItem(item); err != nil {
		log.Printf("API: error updating item status for %s: %v", req.ItemID, err)
	}

	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, claim)
}

// --- helpers ---

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
