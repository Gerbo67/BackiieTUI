package redis

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"BackiieTUI/domain/entities"
	"BackiieTUI/domain/ports"
	"github.com/redis/go-redis/v9"
)

// Adapter implements ports.DatabaseAdapter for Redis.
type Adapter struct {
	client   *redis.Client
	instance *entities.Instance
}

func New(inst *entities.Instance) (*Adapter, error) {
	opts := &redis.Options{
		Addr:        fmt.Sprintf("%s:%d", inst.Host, inst.Port),
		DialTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second,
	}
	if inst.Password != "" {
		opts.Password = inst.Password
	}
	if dbNum, ok := inst.Extra["db"]; ok {
		fmt.Sscanf(dbNum, "%d", &opts.DB)
	}
	return &Adapter{client: redis.NewClient(opts), instance: inst}, nil
}

func (a *Adapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx).Err()
}

// ListDatabases returns logical database indices (0..15) that have keys.
func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	info, err := a.client.Info(ctx, "keyspace").Result()
	if err != nil {
		return nil, err
	}
	var dbs []string
	for _, line := range splitLines(info) {
		if len(line) > 2 && line[0] == 'd' && line[1] == 'b' {
			// e.g. "db0:keys=3,expires=0,avg_ttl=0"
			parts := splitColon(line)
			if len(parts) > 0 {
				dbs = append(dbs, parts[0])
			}
		}
	}
	if len(dbs) == 0 {
		dbs = append(dbs, "db0")
	}
	return dbs, nil
}

// Backup dumps all keys from the current database as JSON, gzip-compressed.
func (a *Adapter) Backup(ctx context.Context, database string) (io.ReadCloser, ports.BackupMeta, error) {
	ts := time.Now().UTC().Format("20060102_150405")
	key := fmt.Sprintf("%s/%s/%s_%s.json.gz",
		string(entities.EngineRedis), a.instance.Name, database, ts)

	meta := ports.BackupMeta{
		DatabaseName: database,
		SuggestedKey: key,
	}

	// Scan all keys
	dump := make(map[string]string)
	var cursor uint64
	for {
		keys, newCursor, err := a.client.Scan(ctx, cursor, "*", 500).Result()
		if err != nil {
			return nil, meta, fmt.Errorf("scan keys: %w", err)
		}
		for _, k := range keys {
			val, err := a.client.Dump(ctx, k).Result()
			if err != nil {
				continue
			}
			dump[k] = val
		}
		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	raw, err := json.Marshal(dump)
	if err != nil {
		return nil, meta, err
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return nil, meta, err
	}
	if err := gz.Close(); err != nil {
		return nil, meta, err
	}

	return io.NopCloser(&buf), meta, nil
}

// Restore loads a backup into Redis by replaying DUMP/RESTORE for every key.
// The reader contains gzip-compressed JSON: map[string]string (key → DUMP value).
func (a *Adapter) Restore(ctx context.Context, _ string, reader io.Reader, _ ports.RestoreOptions) error {
	gr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("descomprimir backup redis: %w", err)
	}
	defer gr.Close()

	raw, err := io.ReadAll(gr)
	if err != nil {
		return fmt.Errorf("leer backup redis: %w", err)
	}

	var dump map[string]string
	if err := json.Unmarshal(raw, &dump); err != nil {
		return fmt.Errorf("parsear backup redis: %w", err)
	}

	for key, dumpVal := range dump {
		if err := a.client.RestoreReplace(ctx, key, 0, dumpVal).Err(); err != nil {
			return fmt.Errorf("restaurar clave %q: %w", key, err)
		}
	}
	return nil
}

func (a *Adapter) Close() error {
	return a.client.Close()
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitColon(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
