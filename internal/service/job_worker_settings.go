package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

const jobWorkerKeyPrefix = "jobs.workers."

type JobWorkerSetting struct {
	Kind    string        `json:"kind"`
	Label   string        `json:"label"`
	Value   int           `json:"value"`
	Default int           `json:"default"`
	Source  config.Source `json:"source"`
	EnvVar  string        `json:"env_var,omitempty"`
	Locked  bool          `json:"locked"`
}

type JobWorkerSettings struct {
	Workers         []JobWorkerSetting `json:"workers"`
	RestartRequired bool               `json:"restart_required"`
}

type JobWorkerUpdate struct {
	Workers map[string]int `json:"workers"`
}

func LoadJobWorkersFromDB(ctx context.Context, db *pgxpool.Pool, cfg *config.Config) {
	if cfg == nil || db == nil {
		return
	}
	q := sqlc.New(db)
	for _, kind := range config.JobWorkerKinds() {
		field, ok := cfg.Jobs.Workers[kind]
		if !ok || field.Source != config.SourceDefault {
			continue
		}
		raw, err := q.GetSystemSetting(ctx, jobWorkerKey(kind))
		if err != nil {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err != nil || value < 1 {
			continue
		}
		cfg.Jobs.Workers[kind] = config.Field[int]{Value: value, Source: config.SourceDB}
	}
}

func (a *App) JobWorkerSettings(_ context.Context) (JobWorkerSettings, error) {
	workers := make([]JobWorkerSetting, 0, len(config.DefaultJobWorkerCounts))
	for _, kind := range config.JobWorkerKinds() {
		def := config.DefaultJobWorkerCounts[kind]
		field := config.Field[int]{Value: def, Source: config.SourceDefault}
		if a.config != nil && a.config.Jobs.Workers != nil {
			if f, ok := a.config.Jobs.Workers[kind]; ok {
				field = f
			}
		}
		envVar, locked := field.EnvLock()
		workers = append(workers, JobWorkerSetting{
			Kind:    kind,
			Label:   jobWorkerLabel(kind),
			Value:   field.Value,
			Default: def,
			Source:  field.Source,
			EnvVar:  envVar,
			Locked:  locked,
		})
	}
	return JobWorkerSettings{Workers: workers, RestartRequired: true}, nil
}

func (a *App) SaveJobWorkerSettings(ctx context.Context, update JobWorkerUpdate) error {
	if len(update.Workers) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(update.Workers))
	for kind := range update.Workers {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		value := update.Workers[kind]
		if _, ok := config.DefaultJobWorkerCounts[kind]; !ok {
			return fmt.Errorf("unknown worker queue %q", kind)
		}
		if value < 1 || value > 64 {
			return fmt.Errorf("%s workers must be between 1 and 64", kind)
		}
		field := a.config.Jobs.Workers[kind]
		if err := errIfEnvLockedChanged(jobWorkerKey(kind), field, value); err != nil {
			return err
		}
	}

	for _, kind := range kinds {
		value := update.Workers[kind]
		field := a.config.Jobs.Workers[kind]
		if field.Source == config.SourceEnv {
			continue
		}
		if err := writeSetting(a, ctx, jobWorkerKey(kind), value); err != nil {
			return err
		}
		a.config.Jobs.Workers[kind] = config.Field[int]{Value: value, Source: config.SourceDB}
	}
	return nil
}

func jobWorkerKey(kind string) string {
	return jobWorkerKeyPrefix + kind
}

func jobWorkerLabel(kind string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ")
	words := strings.Fields(replacer.Replace(kind))
	for i, word := range words {
		if word == "nfo" || word == "smb" {
			words[i] = strings.ToUpper(word)
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
