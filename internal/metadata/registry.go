package metadata

import "sync"

type Registry struct {
	mu       sync.RWMutex
	providers map[string]Provider
	artwork   map[string]ArtworkProvider
	ratings   map[string]RatingsProvider
}

type ProviderInfo struct {
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	Kinds       []MediaKind `json:"kinds"`
	Type        string      `json:"type"`
	NeedsAPIKey bool        `json:"needs_api_key"`
	Configured  bool        `json:"configured"`
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		artwork:   make(map[string]ArtworkProvider),
		ratings:   make(map[string]RatingsProvider),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) RegisterArtwork(p ArtworkProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artwork[p.Name()] = p
}

func (r *Registry) RegisterRatings(p RatingsProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ratings[p.Name()] = p
}

func (r *Registry) Provider(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) Providers(names []string, kind MediaKind) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(names) == 0 {
		return r.allProviders(kind)
	}

	var result []Provider
	for _, name := range names {
		if p, ok := r.providers[name]; ok && p.Supports(kind) {
			result = append(result, p)
		}
	}
	return result
}

func (r *Registry) ArtworkProviders(names []string, kind MediaKind) []ArtworkProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(names) == 0 {
		return r.allArtworkProviders(kind)
	}

	var result []ArtworkProvider
	for _, name := range names {
		if p, ok := r.artwork[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

func (r *Registry) RatingsProviders(names []string) []RatingsProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(names) == 0 {
		var all []RatingsProvider
		for _, p := range r.ratings {
			all = append(all, p)
		}
		return all
	}

	var result []RatingsProvider
	for _, name := range names {
		if p, ok := r.ratings[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

func (r *Registry) allProviders(kind MediaKind) []Provider {
	var result []Provider
	for _, p := range r.providers {
		if p.Supports(kind) {
			result = append(result, p)
		}
	}
	return result
}

func (r *Registry) allArtworkProviders(kind MediaKind) []ArtworkProvider {
	var result []ArtworkProvider
	for _, p := range r.artwork {
		result = append(result, p)
	}
	return result
}

func (r *Registry) AllProviders() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

func (r *Registry) Available() []ProviderInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []ProviderInfo
	for _, p := range r.providers {
		info := ProviderInfo{
			Name:       p.Name(),
			Type:       "metadata",
			Configured: true,
		}
		for _, k := range []MediaKind{KindMovie, KindTV, KindMusic, KindBook} {
			if p.Supports(k) {
				info.Kinds = append(info.Kinds, k)
			}
		}
		infos = append(infos, info)
	}
	for name := range r.artwork {
		if _, exists := r.providers[name]; exists {
			continue
		}
		infos = append(infos, ProviderInfo{
			Name:       name,
			Type:       "artwork",
			Configured: true,
		})
	}
	for name := range r.ratings {
		infos = append(infos, ProviderInfo{
			Name:       name,
			Type:       "ratings",
			Configured: true,
		})
	}
	return infos
}
