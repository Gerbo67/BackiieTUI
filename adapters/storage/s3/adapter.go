package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// Adapter implements ports.StorageAdapter backed by AWS S3 (or compatible).
type Adapter struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string
}

// New creates an S3 Adapter from the given S3Config.
func New(cfg *entities.S3Config) (*Adapter, error) {
	optFns := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		return nil, fmt.Errorf("cargar config AWS: %w", err)
	}

	clientOpts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts,
			func(o *s3.Options) {
				o.BaseEndpoint = aws.String(ensureScheme(cfg.Endpoint))
				o.UsePathStyle = cfg.ForcePathStyle
			},
		)
	}

	client := s3.NewFromConfig(awsCfg, clientOpts...)
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 10 * 1024 * 1024 // 10 MB parts
		u.Concurrency = 4
	})

	return &Adapter{
		client:   client,
		uploader: uploader,
		bucket:   cfg.Bucket,
		prefix:   strings.TrimSuffix(cfg.PathPrefix, "/"),
	}, nil
}

func ensureScheme(endpoint string) string {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	return "https://" + endpoint
}

func (a *Adapter) fullKey(key string) string {
	if a.prefix == "" {
		return key
	}
	return a.prefix + "/" + key
}

// Upload streams body to S3 using multipart upload.
func (a *Adapter) Upload(ctx context.Context, key string, body io.Reader) (int64, error) {
	fk := a.fullKey(key)

	// Use a counting reader to measure bytes uploaded
	cr := &countingReader{r: body}

	_, err := a.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(fk),
		Body:   cr,
	})
	if err != nil {
		return 0, fmt.Errorf("upload S3 %q: %w", fk, err)
	}
	return cr.n, nil
}

// ListObjects lists all objects under the given prefix.
func (a *Adapter) ListObjects(ctx context.Context, prefix string) ([]ports.StorageObject, error) {
	fullPrefix := a.fullKey(prefix)
	var result []ports.StorageObject

	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(a.bucket),
		Prefix: aws.String(fullPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listar objetos S3: %w", err)
		}
		for _, obj := range page.Contents {
			result = append(result, ports.StorageObject{
				Key:          aws.ToString(obj.Key),
				SizeBytes:    aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
				ETag:         aws.ToString(obj.ETag),
			})
		}
	}
	return result, nil
}

// Download streams the object at key from S3 to the caller.
func (a *Adapter) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	fk := a.fullKey(key)
	resp, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(fk),
	})
	if err != nil {
		return nil, fmt.Errorf("descargar S3 %q: %w", fk, err)
	}
	return resp.Body, nil
}

// DeleteObject removes the object at key.
func (a *Adapter) DeleteObject(ctx context.Context, key string) error {
	fk := a.fullKey(key)
	_, err := a.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(fk),
	})
	if err != nil {
		return fmt.Errorf("eliminar S3 %q: %w", fk, err)
	}
	return nil
}

// TagExpiry adds a tag so S3 lifecycle rules can expire the object.
func (a *Adapter) TagExpiry(ctx context.Context, key string, expiresAt time.Time) error {
	fk := a.fullKey(key)
	_, err := a.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(fk),
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{Key: aws.String("expires"), Value: aws.String(expiresAt.UTC().Format("2006-01-02"))},
				{Key: aws.String("managed-by"), Value: aws.String("backiie")},
			},
		},
	})
	return err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
