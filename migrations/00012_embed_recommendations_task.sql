-- +goose Up

-- Scheduled self-heal sweep for recommendation embeddings (doc-hash staleness
-- from 00011 needs something to actually run it). Enabled by default: the
-- kickoff is a clean, near-free no-op while the embedding engine is disabled
-- or when nothing changed.
INSERT INTO public.scheduled_tasks (id, display_name, description, category, enabled, interval_hours, daily_start_time, daily_end_time, max_runtime_minutes) VALUES
('embed_recommendations', 'Refresh Recommendation Embeddings', 'Self-heal sweep for semantic-search embeddings: re-embeds any movie, series, or episode whose metadata changed since it was last embedded. No-op while the embedding engine is disabled or when nothing changed.', 'library', true, 24, '02:00', '06:00', 240)
ON CONFLICT (id) DO NOTHING;

-- +goose Down

DELETE FROM public.scheduled_tasks WHERE id = 'embed_recommendations';
