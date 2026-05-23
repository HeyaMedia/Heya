package opensubtitles

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	User    UserInfo `json:"user"`
	Token   string   `json:"token"`
	Status  int      `json:"status"`
	BaseURL string   `json:"base_url,omitempty"`
}

type UserInfo struct {
	UserID              int    `json:"user_id"`
	AllowedDownloads    int    `json:"allowed_downloads"`
	AllowedTranslations int    `json:"allowed_translations"`
	Level               string `json:"level"`
	VIP                 bool   `json:"vip"`
	ExtInstalled        bool   `json:"ext_installed"`
	RemainingDownloads  int    `json:"remaining_downloads"`
}

type UserInfoResponse struct {
	Data UserInfo `json:"data"`
}

type SearchParams struct {
	IMDbID    string
	TMDbID    string
	Query     string
	Languages []string
	Season    int
	Episode   int
	Type      string
}

type SearchResponse struct {
	TotalPages int              `json:"total_pages"`
	TotalCount int              `json:"total_count"`
	Page       int              `json:"page"`
	Data       []SubtitleResult `json:"data"`
}

type SubtitleResult struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Attributes SubtitleAttributes `json:"attributes"`
}

type SubtitleAttributes struct {
	SubtitleID        string         `json:"subtitle_id"`
	Language          string         `json:"language"`
	DownloadCount     int            `json:"download_count"`
	NewDownloadCount  int            `json:"new_download_count"`
	HearingImpaired   bool           `json:"hearing_impaired"`
	HD                bool           `json:"hd"`
	ForeignPartsOnly  bool           `json:"foreign_parts_only"`
	FPS               float64        `json:"fps"`
	Votes             int            `json:"votes"`
	Ratings           float64        `json:"ratings"`
	FromTrusted       bool           `json:"from_trusted"`
	AITranslated      bool           `json:"ai_translated"`
	MachineTranslated bool           `json:"machine_translated"`
	Release           string         `json:"release"`
	UploadDate        string         `json:"upload_date"`
	Uploader          Uploader       `json:"uploader"`
	FeatureDetails    FeatureDetails `json:"feature_details"`
	Files             []SubFile      `json:"files"`
}

type Uploader struct {
	UploaderID int    `json:"uploader_id"`
	Name       string `json:"name"`
	Rank       string `json:"rank"`
}

type FeatureDetails struct {
	FeatureID   int    `json:"feature_id"`
	FeatureType string `json:"feature_type"`
	Year        int    `json:"year"`
	Title       string `json:"title"`
	MovieName   string `json:"movie_name"`
	IMDbID      int    `json:"imdb_id"`
	TMDbID      int    `json:"tmdb_id"`
}

type SubFile struct {
	FileID   int    `json:"file_id"`
	FileName string `json:"file_name"`
}

type DownloadRequest struct {
	FileID    int    `json:"file_id"`
	SubFormat string `json:"sub_format,omitempty"`
}

type DownloadResponse struct {
	Link         string `json:"link"`
	FileName     string `json:"file_name"`
	Remaining    int    `json:"remaining"`
	Requests     int    `json:"requests"`
	ResetTime    string `json:"reset_time"`
	ResetTimeUTC string `json:"reset_time_utc"`
	Message      string `json:"message"`
}
