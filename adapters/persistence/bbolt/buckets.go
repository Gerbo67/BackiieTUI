package bbolt

// Bucket name constants.
var (
	bucketInstances       = []byte("instances")
	bucketBackupRecords   = []byte("backup_records")
	bucketS3Config        = []byte("s3_config")
	bucketRetention       = []byte("retention")
	bucketSchedulerConfig = []byte("scheduler_config")
	bucketRestoreRecords  = []byte("restore_records")
)

var allBuckets = [][]byte{
	bucketInstances,
	bucketBackupRecords,
	bucketS3Config,
	bucketRetention,
	bucketSchedulerConfig,
	bucketRestoreRecords,
}
