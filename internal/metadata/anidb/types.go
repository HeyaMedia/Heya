package anidb

import "encoding/xml"

type animeResponse struct {
	XMLName     xml.Name       `xml:"anime"`
	ID          int            `xml:"id,attr"`
	Type        string         `xml:"type"`
	EpisodeCount int           `xml:"episodecount"`
	StartDate   string         `xml:"startdate"`
	EndDate     string         `xml:"enddate"`
	Titles      []animeTitle   `xml:"titles>title"`
	Description string         `xml:"description"`
	Picture     string         `xml:"picture"`
	Ratings     animeRatings   `xml:"ratings"`
	Tags        []animeTag     `xml:"tags>tag"`
	Episodes    []animeEpisode `xml:"episodes>episode"`
	Characters  []animeChar    `xml:"characters>character"`
	Creators    []animeCreator `xml:"creators>name"`
}

type animeTitle struct {
	Lang  string `xml:"lang,attr"`
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type animeRatings struct {
	Permanent ratingValue `xml:"permanent"`
	Temporary ratingValue `xml:"temporary"`
}

type ratingValue struct {
	Count int     `xml:"count,attr"`
	Value float64 `xml:",chardata"`
}

type animeTag struct {
	ID     int    `xml:"id,attr"`
	Weight int    `xml:"weight,attr"`
	Name   string `xml:"name"`
}

type animeEpisode struct {
	ID      int              `xml:"id,attr"`
	EpNo    animeEpNo        `xml:"epno"`
	Length  int              `xml:"length"`
	AirDate string           `xml:"airdate"`
	Rating  *ratingValue     `xml:"rating"`
	Titles  []animeEpTitle   `xml:"title"`
}

type animeEpNo struct {
	Type  int    `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type animeEpTitle struct {
	Lang  string `xml:"lang,attr"`
	Value string `xml:",chardata"`
}

type animeChar struct {
	ID       int             `xml:"id,attr"`
	Type     string          `xml:"type,attr"`
	Name     string          `xml:"name"`
	Gender   string          `xml:"gender"`
	Seiyuu   *animeSeiyuu    `xml:"seiyuu"`
	Picture  string          `xml:"charactertype>picture"`
}

type animeSeiyuu struct {
	ID      int    `xml:"id,attr"`
	Name    string `xml:",chardata"`
	Picture string `xml:"picture,attr"`
}

type animeCreator struct {
	ID   int    `xml:"id,attr"`
	Type string `xml:"type,attr"`
	Name string `xml:",chardata"`
}

const (
	epTypeRegular = 1
	epTypeSpecial = 2
	epTypeCredit  = 3
	epTypeTrailer = 4
	epTypeParody  = 5
	epTypeOther   = 6
)

const imageBaseURL = "https://cdn.anidb.net/images/main/"
