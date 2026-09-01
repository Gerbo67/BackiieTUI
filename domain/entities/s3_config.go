package entities

// S3Config holds credentials and settings for S3-compatible storage.
type S3Config struct {
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"` // empty = AWS; set for MinIO/Ceph
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	PathPrefix      string `json:"path_prefix"`      // e.g. "backups/"
	ForcePathStyle  bool   `json:"force_path_style"` // required for MinIO
}
