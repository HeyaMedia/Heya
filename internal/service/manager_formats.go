package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/manager/arr"
	"github.com/karbowiak/heya/internal/manager/formats"
)

// Custom formats: the TRaSH-compatible condition bundles quality profiles
// score. CRUD is thin; the interesting paths are import (live arr instance or
// pasted JSON) and the release tester that runs the evaluator end to end.

func valueOrZero[T any](ptr *T) T {
	var zero T
	if ptr == nil {
		return zero
	}
	return *ptr
}

func valueOr[T any](ptr *T, fallback T) T {
	if ptr == nil {
		return fallback
	}
	return *ptr
}

func nonZeroFormatScores(scores []ManagerFormatScore) []ManagerFormatScore {
	kept := make([]ManagerFormatScore, 0, len(scores))
	for _, score := range scores {
		if score.Score != 0 {
			kept = append(kept, score)
		}
	}
	return kept
}

type ManagerCustomFormatView struct {
	ID                  int64                      `json:"id"`
	Name                string                     `json:"name"`
	Domain              string                     `json:"domain"`
	IncludeWhenRenaming bool                       `json:"include_when_renaming"`
	Specifications      []formats.CustomFormatSpec `json:"specifications"`
	TrashID             string                     `json:"trash_id"`
	Source              string                     `json:"source"`
}

func managerCustomFormatView(row sqlc.ManagerCustomFormat) ManagerCustomFormatView {
	view := ManagerCustomFormatView{
		ID:                  row.ID,
		Name:                row.Name,
		Domain:              row.Domain,
		IncludeWhenRenaming: row.IncludeWhenRenaming,
		Specifications:      []formats.CustomFormatSpec{},
		TrashID:             row.TrashID,
		Source:              row.Source,
	}
	_ = json.Unmarshal(row.Specifications, &view.Specifications)
	return view
}

func (a *App) ListManagerCustomFormats(ctx context.Context) ([]ManagerCustomFormatView, error) {
	rows, err := sqlc.New(a.db).ListManagerCustomFormats(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manager custom formats: %w", err)
	}
	views := make([]ManagerCustomFormatView, 0, len(rows))
	for _, row := range rows {
		views = append(views, managerCustomFormatView(row))
	}
	return views, nil
}

type ManagerCustomFormatInput struct {
	Name                string                     `json:"name"`
	Domain              string                     `json:"domain"`
	IncludeWhenRenaming bool                       `json:"include_when_renaming,omitempty"`
	Specifications      []formats.CustomFormatSpec `json:"specifications"`
}

func (input *ManagerCustomFormatInput) normalize() error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch input.Domain {
	case "video", "music", "book":
	default:
		return fmt.Errorf("domain must be video, music, or book")
	}
	if len(input.Specifications) == 0 {
		return fmt.Errorf("at least one condition is required")
	}
	for index := range input.Specifications {
		spec := &input.Specifications[index]
		spec.Name = strings.TrimSpace(spec.Name)
		if spec.Name == "" {
			spec.Name = fmt.Sprintf("Condition %d", index+1)
		}
		if spec.Fields == nil {
			spec.Fields = map[string]any{}
		}
		if err := spec.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) CreateManagerCustomFormat(ctx context.Context, input ManagerCustomFormatInput) (ManagerCustomFormatView, error) {
	if err := input.normalize(); err != nil {
		return ManagerCustomFormatView{}, err
	}
	specs, err := json.Marshal(input.Specifications)
	if err != nil {
		return ManagerCustomFormatView{}, fmt.Errorf("encoding specifications: %w", err)
	}
	row, err := sqlc.New(a.db).CreateManagerCustomFormat(ctx, sqlc.CreateManagerCustomFormatParams{
		Name:                input.Name,
		Domain:              input.Domain,
		IncludeWhenRenaming: input.IncludeWhenRenaming,
		Specifications:      specs,
	})
	if err != nil {
		return ManagerCustomFormatView{}, fmt.Errorf("create manager custom format: %w", err)
	}
	a.notifyManagerChanged(ctx, "custom_formats")
	return managerCustomFormatView(row), nil
}

func (a *App) UpdateManagerCustomFormat(ctx context.Context, id int64, input ManagerCustomFormatInput) (ManagerCustomFormatView, error) {
	q := sqlc.New(a.db)
	existing, err := q.GetManagerCustomFormat(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ManagerCustomFormatView{}, ErrManagerNotFound
		}
		return ManagerCustomFormatView{}, fmt.Errorf("get manager custom format: %w", err)
	}
	input.Domain = existing.Domain
	if err := input.normalize(); err != nil {
		return ManagerCustomFormatView{}, err
	}
	specs, err := json.Marshal(input.Specifications)
	if err != nil {
		return ManagerCustomFormatView{}, fmt.Errorf("encoding specifications: %w", err)
	}
	row, err := q.UpdateManagerCustomFormat(ctx, sqlc.UpdateManagerCustomFormatParams{
		ID:                  id,
		Name:                input.Name,
		IncludeWhenRenaming: input.IncludeWhenRenaming,
		Specifications:      specs,
		TrashID:             existing.TrashID,
		Source:              existing.Source,
	})
	if err != nil {
		return ManagerCustomFormatView{}, fmt.Errorf("update manager custom format: %w", err)
	}
	a.notifyManagerChanged(ctx, "custom_formats")
	return managerCustomFormatView(row), nil
}

func (a *App) DeleteManagerCustomFormat(ctx context.Context, id int64) error {
	q := sqlc.New(a.db)
	if _, err := q.GetManagerCustomFormat(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrManagerNotFound
		}
		return fmt.Errorf("get manager custom format: %w", err)
	}
	if err := q.DeleteManagerCustomFormat(ctx, id); err != nil {
		return fmt.Errorf("delete manager custom format: %w", err)
	}
	// Dangling profile references score zero, matching arr behavior; profile
	// editors drop them on next save.
	a.notifyManagerChanged(ctx, "custom_formats")
	return nil
}

// ── Import ───────────────────────────────────────────────────────────────

type ManagerFormatImportInput struct {
	// 'radarr' | 'sonarr' | 'lidarr' — decides API version, enum tables, and
	// target domain (video for the first two, music for lidarr).
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	// Pasted JSON alternative to a live instance: an arr export, a TRaSH
	// guide format, or an array of either.
	JSON string `json:"json,omitempty"`
	// Also import quality profiles (live instances only) with their tuned
	// format scores.
	IncludeProfiles bool `json:"include_profiles,omitempty"`
}

type ManagerFormatImportResult struct {
	Created         int      `json:"created"`
	Updated         int      `json:"updated"`
	Skipped         []string `json:"skipped"`
	Warnings        []string `json:"warnings"`
	ProfilesCreated []string `json:"profiles_created"`
	ProfilesUpdated []string `json:"profiles_updated"`
	ProfilesSkipped []string `json:"profiles_skipped"`
}

func importDomainForKind(kind string) (string, error) {
	switch kind {
	case "radarr", "sonarr":
		return "video", nil
	case "lidarr":
		return "music", nil
	default:
		return "", fmt.Errorf("kind must be radarr, sonarr, or lidarr")
	}
}

// ImportManagerCustomFormats pulls custom formats (and optionally quality
// profiles) from a live arr instance, or normalizes a pasted JSON payload.
func (a *App) ImportManagerCustomFormats(ctx context.Context, input ManagerFormatImportInput) (ManagerFormatImportResult, error) {
	result := ManagerFormatImportResult{
		Skipped: []string{}, Warnings: []string{},
		ProfilesCreated: []string{}, ProfilesUpdated: []string{}, ProfilesSkipped: []string{},
	}
	domain, err := importDomainForKind(input.Kind)
	if err != nil {
		return result, err
	}

	var rawFormats []byte
	var client *arr.Client
	switch {
	case strings.TrimSpace(input.JSON) != "":
		rawFormats = []byte(input.JSON)
	case input.BaseURL != "":
		baseURL, err := validateManagerBaseURL(input.BaseURL)
		if err != nil {
			return result, err
		}
		client = arr.New(input.Kind, baseURL, input.APIKey)
		rawFormats, err = client.CustomFormatsRaw(ctx)
		if err != nil {
			return result, err
		}
	default:
		return result, fmt.Errorf("provide either a base_url or pasted JSON")
	}

	imported, err := formats.ParseArrFormats(input.Kind, rawFormats)
	if err != nil {
		return result, err
	}

	q := sqlc.New(a.db)
	for _, format := range imported {
		for _, warning := range format.Warnings {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", format.Name, warning))
		}
		specs, err := json.Marshal(format.Specs)
		if err != nil {
			return result, fmt.Errorf("encoding specifications for %q: %w", format.Name, err)
		}
		existing, err := q.GetManagerCustomFormatByName(ctx, sqlc.GetManagerCustomFormatByNameParams{Domain: domain, Name: format.Name})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := q.CreateManagerCustomFormat(ctx, sqlc.CreateManagerCustomFormatParams{
				Name:                format.Name,
				Domain:              domain,
				IncludeWhenRenaming: format.IncludeWhenRenaming,
				Specifications:      specs,
				TrashID:             format.TrashID,
				Source:              input.Kind,
			}); err != nil {
				return result, fmt.Errorf("creating %q: %w", format.Name, err)
			}
			result.Created++
		case err != nil:
			return result, fmt.Errorf("looking up %q: %w", format.Name, err)
		case existing.Source == input.Kind || (format.TrashID != "" && existing.TrashID == format.TrashID):
			if _, err := q.UpdateManagerCustomFormat(ctx, sqlc.UpdateManagerCustomFormatParams{
				ID:                  existing.ID,
				Name:                format.Name,
				IncludeWhenRenaming: format.IncludeWhenRenaming,
				Specifications:      specs,
				TrashID:             format.TrashID,
				Source:              input.Kind,
			}); err != nil {
				return result, fmt.Errorf("updating %q: %w", format.Name, err)
			}
			result.Updated++
		default:
			// Same name from a different app (Radarr and Sonarr share most
			// TRaSH names): first import wins, the collision is reported.
			result.Skipped = append(result.Skipped, format.Name)
		}
	}

	if input.IncludeProfiles && client != nil {
		if err := a.importArrProfiles(ctx, client, input.Kind, domain, &result); err != nil {
			return result, err
		}
	}

	a.notifyManagerChanged(ctx, "custom_formats")
	if len(result.ProfilesCreated) > 0 || len(result.ProfilesUpdated) > 0 {
		a.notifyManagerChanged(ctx, "quality_profiles")
	}
	return result, nil
}

func (a *App) importArrProfiles(ctx context.Context, client *arr.Client, kind, domain string, result *ManagerFormatImportResult) error {
	rawProfiles, err := client.QualityProfilesRaw(ctx)
	if err != nil {
		return err
	}
	profiles, err := formats.ParseArrProfiles(rawProfiles)
	if err != nil {
		return err
	}

	q := sqlc.New(a.db)
	formatRows, err := q.ListManagerCustomFormats(ctx)
	if err != nil {
		return fmt.Errorf("list custom formats: %w", err)
	}
	idsByName := make(map[string]int64, len(formatRows))
	for _, row := range formatRows {
		if row.Domain == domain {
			idsByName[row.Name] = row.ID
		}
	}

	existingProfiles, err := q.ListManagerQualityProfiles(ctx)
	if err != nil {
		return fmt.Errorf("list quality profiles: %w", err)
	}
	takenNames := make(map[string]bool, len(existingProfiles))
	// A profile imported earlier from this same app (under its plain or
	// suffixed name) is updated in place — re-imports sync, never duplicate.
	ownRowByName := make(map[string]sqlc.ManagerQualityProfile)
	for _, row := range existingProfiles {
		takenNames[row.Name] = true
		if row.Source == kind {
			ownRowByName[row.Name] = row
		}
	}

	for _, profile := range profiles {
		suffixed := fmt.Sprintf("%s (%s)", profile.Name, kind)
		existing, isReimport := ownRowByName[profile.Name]
		if !isReimport {
			existing, isReimport = ownRowByName[suffixed]
		}

		name := profile.Name
		if !isReimport {
			if takenNames[name] {
				name = suffixed
			}
			if takenNames[name] {
				result.ProfilesSkipped = append(result.ProfilesSkipped, profile.Name)
				continue
			}
		}

		items := make([]ManagerQualityItem, 0, len(profile.Items))
		for _, item := range profile.Items {
			items = append(items, ManagerQualityItem{
				Quality:   item.Quality,
				Group:     item.Group,
				Qualities: item.Qualities,
				Allowed:   item.Allowed,
			})
		}
		scores := make([]ManagerFormatScore, 0, len(profile.FormatScores))
		for formatName, score := range profile.FormatScores {
			id, ok := idsByName[formatName]
			if !ok {
				result.Warnings = append(result.Warnings, fmt.Sprintf("profile %q: no custom format named %q; its score was dropped", profile.Name, formatName))
				continue
			}
			scores = append(scores, ManagerFormatScore{FormatID: id, Score: int32(score)})
		}

		cutoff := profile.Cutoff
		if cutoff == "" {
			for _, item := range items {
				if item.Allowed {
					cutoff = item.Quality
					if item.Group != "" {
						cutoff = item.Group
					}
					break
				}
			}
		}

		itemsJSON, err := json.Marshal(items)
		if err != nil {
			return fmt.Errorf("encoding items for %q: %w", profile.Name, err)
		}
		scoresJSON, err := json.Marshal(scores)
		if err != nil {
			return fmt.Errorf("encoding scores for %q: %w", profile.Name, err)
		}

		if isReimport {
			if _, err := q.UpdateManagerQualityProfile(ctx, sqlc.UpdateManagerQualityProfileParams{
				ID:                existing.ID,
				Name:              existing.Name,
				Items:             itemsJSON,
				Cutoff:            cutoff,
				UpgradesEnabled:   profile.UpgradesEnabled,
				MinFormatScore:    int32(profile.MinFormatScore),
				CutoffFormatScore: int32(profile.CutoffFormatScore),
				MinUpgradeScore:   int32(profile.MinUpgradeScore),
				FormatScores:      scoresJSON,
				Language:          profile.Language,
			}); err != nil {
				return fmt.Errorf("updating profile %q: %w", existing.Name, err)
			}
			result.ProfilesUpdated = append(result.ProfilesUpdated, existing.Name)
			continue
		}

		if _, err := q.CreateManagerQualityProfile(ctx, sqlc.CreateManagerQualityProfileParams{
			Name:              name,
			Domain:            domain,
			Items:             itemsJSON,
			Cutoff:            cutoff,
			UpgradesEnabled:   profile.UpgradesEnabled,
			MinFormatScore:    int32(profile.MinFormatScore),
			CutoffFormatScore: int32(profile.CutoffFormatScore),
			MinUpgradeScore:   int32(profile.MinUpgradeScore),
			FormatScores:      scoresJSON,
			Source:            kind,
			Language:          profile.Language,
		}); err != nil {
			return fmt.Errorf("creating profile %q: %w", name, err)
		}
		takenNames[name] = true
		result.ProfilesCreated = append(result.ProfilesCreated, name)
	}
	return nil
}

// ── Release tester ───────────────────────────────────────────────────────

type ManagerReleaseTestInput struct {
	Title string `json:"title"`
	// Optional size in bytes for SizeSpecification conditions.
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// Parse as a TV release (season/episode aware) instead of a movie.
	TV bool `json:"tv,omitempty"`
}

type ManagerReleaseTestParsed struct {
	Resolution  int      `json:"resolution"`
	Sources     []string `json:"sources"`
	Modifier    string   `json:"modifier"`
	Group       string   `json:"group"`
	Languages   []string `json:"languages"`
	Year        int      `json:"year"`
	ReleaseType string   `json:"release_type,omitempty"`
}

type ManagerReleaseTestMatch struct {
	FormatID int64  `json:"format_id"`
	Name     string `json:"name"`
}

type ManagerReleaseTestProfileScore struct {
	ProfileID  int64  `json:"profile_id"`
	Name       string `json:"name"`
	Score      int32  `json:"score"`
	MinMet     bool   `json:"min_met"`
	Language   string `json:"language"`
	LanguageOK bool   `json:"language_ok"`
}

type ManagerReleaseTestView struct {
	Parsed   ManagerReleaseTestParsed         `json:"parsed"`
	Matches  []ManagerReleaseTestMatch        `json:"matches"`
	Profiles []ManagerReleaseTestProfileScore `json:"profiles"`
}

// TestManagerRelease parses a release title, evaluates every video-domain
// custom format against it, and scores the result under each video profile —
// the same math the decision engine will run per candidate.
func (a *App) TestManagerRelease(ctx context.Context, input ManagerReleaseTestInput) (ManagerReleaseTestView, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return ManagerReleaseTestView{}, fmt.Errorf("title is required")
	}
	attrs := formats.ParseVideoRelease(title, input.SizeBytes, input.TV)
	view := ManagerReleaseTestView{
		Parsed: ManagerReleaseTestParsed{
			Resolution:  attrs.Resolution,
			Sources:     attrs.Sources,
			Modifier:    attrs.Modifier,
			Group:       attrs.Group,
			Languages:   attrs.Languages,
			Year:        attrs.Year,
			ReleaseType: attrs.ReleaseType,
		},
		Matches:  []ManagerReleaseTestMatch{},
		Profiles: []ManagerReleaseTestProfileScore{},
	}
	if view.Parsed.Sources == nil {
		view.Parsed.Sources = []string{}
	}
	if view.Parsed.Languages == nil {
		view.Parsed.Languages = []string{}
	}

	q := sqlc.New(a.db)
	formatRows, err := q.ListManagerCustomFormats(ctx)
	if err != nil {
		return view, fmt.Errorf("list custom formats: %w", err)
	}
	matched := make(map[int64]bool)
	for _, row := range formatRows {
		if row.Domain != "video" {
			continue
		}
		var specs []formats.CustomFormatSpec
		if err := json.Unmarshal(row.Specifications, &specs); err != nil {
			continue
		}
		if formats.Matches(formats.Format{ID: row.ID, Name: row.Name, Specs: specs}, attrs) {
			matched[row.ID] = true
			view.Matches = append(view.Matches, ManagerReleaseTestMatch{FormatID: row.ID, Name: row.Name})
		}
	}

	profileRows, err := q.ListManagerQualityProfiles(ctx)
	if err != nil {
		return view, fmt.Errorf("list quality profiles: %w", err)
	}
	for _, row := range profileRows {
		if row.Domain != "video" {
			continue
		}
		var scores []ManagerFormatScore
		_ = json.Unmarshal(row.FormatScores, &scores)
		var total int32
		for _, score := range scores {
			if matched[score.FormatID] {
				total += score.Score
			}
		}
		view.Profiles = append(view.Profiles, ManagerReleaseTestProfileScore{
			ProfileID:  row.ID,
			Name:       row.Name,
			Score:      total,
			MinMet:     total >= row.MinFormatScore,
			Language:   row.Language,
			LanguageOK: formats.LanguageAcceptable(row.Language, attrs),
		})
	}
	return view, nil
}
