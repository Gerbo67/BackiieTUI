package s3

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"BackiieTUI/domain/entities"
)

const managedByBackiie = "backiie-"

// GetLifecycleRules returns all lifecycle rules configured on the bucket.
// Returns empty slice (no error) when no lifecycle configuration exists.
func (a *Adapter) GetLifecycleRules(ctx context.Context) ([]entities.LifecycleRule, error) {
	output, err := a.client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(a.bucket),
	})
	if err != nil {
		if isNoSuchLifecycle(err) {
			return nil, nil
		}
		return nil, err
	}

	var rules []entities.LifecycleRule
	for _, r := range output.Rules {
		rule := entities.LifecycleRule{
			ID:     aws.ToString(r.ID),
			Status: string(r.Status),
		}
		// Prefer Filter.Prefix; fall back to deprecated Prefix field
		if r.Filter != nil && r.Filter.Prefix != nil {
			rule.Prefix = aws.ToString(r.Filter.Prefix)
		} else if r.Prefix != nil {
			rule.Prefix = aws.ToString(r.Prefix)
		}
		if r.Expiration != nil && r.Expiration.Days != nil {
			rule.ExpiryDays = aws.ToInt32(r.Expiration.Days)
		}
		if strings.HasPrefix(rule.ID, managedByBackiie) {
			rule.ManagedBy = "backiie"
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// PutLifecycleRules replaces all lifecycle rules on the bucket.
// S3 has no per-rule PUT — the entire configuration is replaced atomically.
// Pass an empty slice to remove all lifecycle rules.
func (a *Adapter) PutLifecycleRules(ctx context.Context, rules []entities.LifecycleRule) error {
	if len(rules) == 0 {
		_, err := a.client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(a.bucket),
		})
		return err
	}

	awsRules := make([]types.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		ar := types.LifecycleRule{
			ID:     aws.String(r.ID),
			Status: types.ExpirationStatus(r.Status),
			Expiration: &types.LifecycleExpiration{
				Days: aws.Int32(r.ExpiryDays),
			},
		}
		// An explicit (even empty) Filter is required by the S3 API v2.
		if r.Prefix != "" {
			ar.Filter = &types.LifecycleRuleFilter{Prefix: aws.String(r.Prefix)}
		} else {
			ar.Filter = &types.LifecycleRuleFilter{}
		}
		awsRules = append(awsRules, ar)
	}

	_, err := a.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(a.bucket),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: awsRules,
		},
	})
	return err
}

// isNoSuchLifecycle returns true when the bucket has no lifecycle configuration.
func isNoSuchLifecycle(err error) bool {
	// AWS SDK v2 wraps S3 errors as smithy APIErrors.
	type apiCoder interface {
		ErrorCode() string
	}
	var ac apiCoder
	if errors.As(err, &ac) {
		code := ac.ErrorCode()
		return code == "NoSuchLifecycleConfiguration" || code == "NoSuchBucketPolicy"
	}
	// Fallback for providers that use plain HTTP 404 strings.
	return strings.Contains(err.Error(), "NoSuchLifecycle") ||
		strings.Contains(err.Error(), "404")
}
