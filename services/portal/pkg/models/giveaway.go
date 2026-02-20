//go:build giveaway

package models

import "time"

// ItemStatus represents the availability state of a giveaway item.
type ItemStatus string

const (
	ItemStatusAvailable ItemStatus = "available"
	ItemStatusClaimed   ItemStatus = "claimed"
	ItemStatusGone      ItemStatus = "gone"
)

// ItemCondition describes the physical state of the item.
type ItemCondition string

const (
	ConditionNew      ItemCondition = "new"
	ConditionLikeNew  ItemCondition = "like_new"
	ConditionGood     ItemCondition = "good"
	ConditionFair     ItemCondition = "fair"
	ConditionPoor     ItemCondition = "poor"
	ConditionForParts ItemCondition = "for_parts"
)

// Item is a giveaway listing.
type Item struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	ImageURL     string        `json:"image_url"`
	Condition    ItemCondition `json:"condition"`
	Status       ItemStatus    `json:"status"`
	DistMiles    float64       `json:"dist_miles"`    // one-way miles from federal building
	DriveMinutes int           `json:"drive_minutes"` // one-way estimated drive time
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// ClaimStatus represents the lifecycle state of a claim.
type ClaimStatus string

const (
	ClaimStatusPending   ClaimStatus = "pending"
	ClaimStatusConfirmed ClaimStatus = "confirmed"
	ClaimStatusDelivered ClaimStatus = "delivered"
	ClaimStatusCancelled ClaimStatus = "cancelled"
)

// Claim is a request to receive a giveaway item.
type Claim struct {
	ID           string      `json:"id"`
	ItemID       string      `json:"item_id"`
	ClaimerName  string      `json:"claimer_name"`
	ClaimerEmail string      `json:"claimer_email"`
	ClaimerPhone string      `json:"claimer_phone"`
	DeliveryFee  float64     `json:"delivery_fee"`
	Status       ClaimStatus `json:"status"`
	Notes        string      `json:"notes"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}
