package server

type ErrorModel struct {
	Detail string `json:"detail" doc:"Error description"`
}

type HealthOutput struct {
	Body struct {
		Status   string `json:"status" example:"ok" doc:"Server status"`
		Database string `json:"database" example:"connected" doc:"Database connection status"`
	}
}

type RegisterInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" doc:"Username"`
		Email    string `json:"email" doc:"Email address"`
		Password string `json:"password" minLength:"1" doc:"Password"`
	}
}

type LoginInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" doc:"Username"`
		Password string `json:"password" minLength:"1" doc:"Password"`
	}
}

type AuthTokenOutput struct {
	Body struct {
		Token string   `json:"token" doc:"Session token"`
		User  userView `json:"user" doc:"User details"`
	}
}

type LibraryIDParam struct {
	ID int64 `path:"id" doc:"Library ID"`
}

type CreateLibraryInput struct {
	Body struct {
		Name      string   `json:"name" minLength:"1" doc:"Library display name"`
		MediaType string   `json:"media_type" enum:"movie,tv,music,book,comic,podcast,radio" doc:"Media type"`
		Paths     []string `json:"paths" minItems:"1" doc:"Filesystem paths"`
	}
}

type PaginationParams struct {
	Limit  int32 `query:"limit" default:"50" doc:"Max results to return"`
	Offset int32 `query:"offset" default:"0" doc:"Results offset"`
}

type FileStatusFilter struct {
	Status string `query:"status" doc:"Filter by file status"`
}

type MediaTypeFilter struct {
	Type string `query:"type" doc:"Filter by media type (movie, tv, music, book)"`
}

type SearchQueryParam struct {
	Q string `query:"q" minLength:"1" doc:"Search query"`
}

type AsyncParam struct {
	Async bool `query:"async" doc:"Run asynchronously via job queue"`
}

type MediaIDParam struct {
	ID int64 `path:"id" doc:"Media item ID"`
}

type FileIDParam struct {
	ID int64 `path:"id" doc:"Library file ID"`
}

type ResolveMatchInput struct {
	Body struct {
		CandidateID int64 `json:"candidate_id" doc:"Match candidate ID to accept"`
	}
}
