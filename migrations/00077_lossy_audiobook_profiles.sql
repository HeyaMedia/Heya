-- +goose Up
-- Two more starter profiles: lossy-preferred music and audiobooks. The book
-- domain covers ebooks and audiobooks in one quality vocabulary — a profile
-- picks which side it wants.
INSERT INTO manager_quality_profiles (name, domain, items, cutoff) VALUES
(
    'Lossy', 'music',
    '[{"quality": "mp3-320", "allowed": true},
      {"quality": "mp3-v0", "allowed": true},
      {"quality": "aac-320", "allowed": true},
      {"quality": "mp3-256", "allowed": true},
      {"quality": "mp3-v2", "allowed": false},
      {"quality": "mp3-192", "allowed": false}]'::jsonb,
    'mp3-320'
),
(
    'Audiobooks', 'book',
    '[{"quality": "m4b", "allowed": true},
      {"quality": "flac", "allowed": false},
      {"quality": "mp3-320", "allowed": true},
      {"quality": "mp3-128", "allowed": false}]'::jsonb,
    'm4b'
)
ON CONFLICT (name) DO NOTHING;

-- +goose Down
DELETE FROM manager_quality_profiles WHERE name IN ('Lossy', 'Audiobooks') AND source = '';
