package fanart

type movieResponse struct {
	Name         string     `json:"name"`
	TmdbID       string     `json:"tmdb_id"`
	HDMovieLogo  []artImage `json:"hdmovielogo"`
	MovieLogo    []artImage `json:"movielogo"`
	MoviePoster  []artImage `json:"movieposter"`
	HDMovieClearArt []artImage `json:"hdmovieclearart"`
	MovieBackground []artImage `json:"moviebackground"`
	MovieBanner  []artImage `json:"moviebanner"`
	MovieThumb   []artImage `json:"moviethumb"`
}

type tvResponse struct {
	Name        string     `json:"name"`
	ThetvdbID   string     `json:"thetvdb_id"`
	HDTVLogo    []artImage `json:"hdtvlogo"`
	TVPoster    []artImage `json:"tvposter"`
	TVBanner    []artImage `json:"tvbanner"`
	HDClearArt  []artImage `json:"hdclearart"`
	ShowBackground []artImage `json:"showbackground"`
	TVThumb     []artImage `json:"tvthumb"`
	ClearLogo   []artImage `json:"clearlogo"`
}

type artImage struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}
