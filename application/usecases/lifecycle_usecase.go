package usecases

import (
	"context"
	"fmt"
	"strings"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
)

// LifecycleUseCase manages S3 lifecycle rules and their synchronisation with
// the retention policies configured in BackiieTUI.
type LifecycleUseCase struct {
	storageFactory ports.S3Factory
	retentionRepo  ports.RetentionRepository
	instanceRepo   ports.InstanceRepository
	s3cfgRepo      ports.S3ConfigRepository
}

func NewLifecycleUseCase(
	storageFactory ports.S3Factory,
	retentionRepo ports.RetentionRepository,
	instanceRepo ports.InstanceRepository,
	s3cfgRepo ports.S3ConfigRepository,
) *LifecycleUseCase {
	return &LifecycleUseCase{
		storageFactory: storageFactory,
		retentionRepo:  retentionRepo,
		instanceRepo:   instanceRepo,
		s3cfgRepo:      s3cfgRepo,
	}
}

// lifecycle returns a fresh LifecycleManager built from current S3 config.
func (uc *LifecycleUseCase) lifecycle(ctx context.Context) (ports.LifecycleManager, error) {
	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("S3 no configurado")
	}
	return uc.storageFactory.NewLifecycle(s3cfg)
}

// GetRules returns all current lifecycle rules on the S3 bucket.
func (uc *LifecycleUseCase) GetRules(ctx context.Context) ([]entities.LifecycleRule, error) {
	lm, err := uc.lifecycle(ctx)
	if err != nil {
		return nil, err
	}
	return lm.GetLifecycleRules(ctx)
}

// SyncWithRetention creates/replaces BackiieTUI-managed lifecycle rules based on
// the current retention policies. Rules belonging to other tools are preserved.
//
// Strategy:
//   - One rule per instance that has a custom retention policy (prefix-based).
//   - One global rule for all other objects under the configured S3 prefix.
//   - Existing backiie-* rules are always replaced; non-backiie rules are kept.
func (uc *LifecycleUseCase) SyncWithRetention(ctx context.Context) error {
	lm, err := uc.lifecycle(ctx)
	if err != nil {
		return err
	}

	// 1. Load current rules and keep non-BackiieTUI ones.
	existing, err := lm.GetLifecycleRules(ctx)
	if err != nil {
		return fmt.Errorf("leer reglas actuales: %w", err)
	}
	var external []entities.LifecycleRule
	for _, r := range existing {
		if r.ManagedBy != "backiie" {
			external = append(external, r)
		}
	}

	// 2. Load S3 prefix.
	s3cfg, err := uc.s3cfgRepo.Get(ctx)
	if err != nil {
		return fmt.Errorf("config S3: %w", err)
	}
	s3prefix := strings.TrimSuffix(s3cfg.PathPrefix, "/")

	// 3. Load instances (to resolve instance IDs to names/engines).
	instances, err := uc.instanceRepo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("listar instancias: %w", err)
	}
	instMap := make(map[string]*entities.Instance, len(instances))
	for _, inst := range instances {
		instMap[inst.ID] = inst
	}

	// 4. Load retention policies.
	policies, err := uc.retentionRepo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("listar políticas: %w", err)
	}

	globalDays := entities.DefaultRetentionPolicy().RetainDays
	var newRules []entities.LifecycleRule

	for _, pol := range policies {
		if pol.InstanceID == "" {
			globalDays = pol.RetainDays
			continue
		}
		inst, ok := instMap[pol.InstanceID]
		if !ok {
			continue
		}
		prefix := buildInstancePrefix(s3prefix, string(inst.Engine), inst.Name)
		newRules = append(newRules, entities.LifecycleRule{
			ID:         "backiie-" + sanitizeID(inst.Name),
			Status:     "Enabled",
			Prefix:     prefix,
			ExpiryDays: int32(pol.RetainDays),
			ManagedBy:  "backiie",
		})
	}

	// Global rule covers every object under the S3 prefix (or the whole bucket).
	globalPrefix := s3prefix
	if globalPrefix != "" {
		globalPrefix += "/"
	}
	newRules = append(newRules, entities.LifecycleRule{
		ID:         "backiie-global",
		Status:     "Enabled",
		Prefix:     globalPrefix,
		ExpiryDays: int32(globalDays),
		ManagedBy:  "backiie",
	})

	// 5. Merge and write.
	combined := append(external, newRules...)
	return lm.PutLifecycleRules(ctx, combined)
}

// DeleteRule removes the lifecycle rule with the given ID, preserving all others.
func (uc *LifecycleUseCase) DeleteRule(ctx context.Context, ruleID string) error {
	lm, err := uc.lifecycle(ctx)
	if err != nil {
		return err
	}
	rules, err := lm.GetLifecycleRules(ctx)
	if err != nil {
		return err
	}
	filtered := rules[:0]
	for _, r := range rules {
		if r.ID != ruleID {
			filtered = append(filtered, r)
		}
	}
	return lm.PutLifecycleRules(ctx, filtered)
}

// buildInstancePrefix returns the S3 prefix that covers all backups for an instance.
// Format: [{s3prefix}/]{engine}/{instanceName}/
func buildInstancePrefix(s3prefix, engine, instanceName string) string {
	if s3prefix == "" {
		return fmt.Sprintf("%s/%s/", engine, instanceName)
	}
	return fmt.Sprintf("%s/%s/%s/", s3prefix, engine, instanceName)
}

// sanitizeID strips characters not allowed in S3 lifecycle rule IDs.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
