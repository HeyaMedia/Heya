package openlibrary

type searchResponse struct {
	NumFound int        `json:"numFound"`
	Docs     []searchDoc `json:"docs"`
}

type searchDoc struct {
	Key             string   `json:"key"`
	Title           string   `json:"title"`
	AuthorName      []string `json:"author_name"`
	AuthorKey       []string `json:"author_key"`
	FirstPublishYear int     `json:"first_publish_year"`
	CoverI          int      `json:"cover_i"`
	ISBN            []string `json:"isbn"`
	Subject         []string `json:"subject"`
	Language        []string `json:"language"`
	Publisher       []string `json:"publisher"`
	NumberOfPages   int      `json:"number_of_pages_median"`
}

type workDetail struct {
	Key         string      `json:"key"`
	Title       string      `json:"title"`
	Description interface{} `json:"description"`
	Covers      []int       `json:"covers"`
	Subjects    []string    `json:"subjects"`
	Authors     []authorRef `json:"authors"`
}

type authorRef struct {
	Author struct {
		Key string `json:"key"`
	} `json:"author"`
}

type authorDetail struct {
	Key       string      `json:"key"`
	Name      string      `json:"name"`
	Bio       interface{} `json:"bio"`
	BirthDate string      `json:"birth_date"`
	DeathDate string      `json:"death_date"`
	Photos    []int       `json:"photos"`
}

type isbnResponse struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Authors []struct {
		Key string `json:"key"`
	} `json:"authors"`
	Works []struct {
		Key string `json:"key"`
	} `json:"works"`
	NumberOfPages int      `json:"number_of_pages"`
	Publishers    []string `json:"publishers"`
	PublishDate   string   `json:"publish_date"`
	ISBN13        []string `json:"isbn_13"`
	ISBN10        []string `json:"isbn_10"`
	Covers        []int    `json:"covers"`
}

type editionsResponse struct {
	Entries []editionEntry `json:"entries"`
}

type editionEntry struct {
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	NumberOfPages int      `json:"number_of_pages"`
	Publishers    []string `json:"publishers"`
	PublishDate   string   `json:"publish_date"`
	ISBN13        []string `json:"isbn_13"`
	ISBN10        []string `json:"isbn_10"`
	Covers        []int    `json:"covers"`
	Languages     []struct {
		Key string `json:"key"`
	} `json:"languages"`
}
