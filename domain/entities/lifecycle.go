package entities

// LifecycleRule represents a single S3 lifecycle rule for automatic object expiration.
type LifecycleRule struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // "Enabled" | "Disabled"
	Prefix     string `json:"prefix"` // path prefix filter; empty = all objects in bucket
	ExpiryDays int32  `json:"expiry_days"`
	ManagedBy  string `json:"managed_by"` // "backiie" when created by this app, empty otherwise
}
