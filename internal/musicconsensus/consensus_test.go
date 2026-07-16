package musicconsensus

import "testing"

func TestBuildEightyPercentConsensus(t *testing.T) {
	evidence := make([]Evidence, 10)
	for i := 0; i < 8; i++ {
		evidence[i] = Evidence{Artist: "Asaco", Album: "Nomake Story", Year: "2020"}
	}
	evidence[8] = Evidence{Artist: "DJ Paul", Album: "To Kill Again...The Mixtape", Year: "2010"}
	// The tenth track has no release-level tags and should inherit the winner.

	got := Build(evidence)
	if !got.Artist.Strong || got.Artist.Value != "Asaco" || got.Artist.Support != 8 || got.Artist.Usable != 9 || got.Artist.Missing != 1 {
		t.Fatalf("artist consensus = %#v", got.Artist)
	}
	if !got.Album.Strong || got.Album.Value != "Nomake Story" {
		t.Fatalf("album consensus = %#v", got.Album)
	}
	if !got.Year.Strong || got.Year.Value != "2020" {
		t.Fatalf("year consensus = %#v", got.Year)
	}
	if got.Artist.Matches("DJ Paul") || !got.Artist.Matches("asaco") {
		t.Fatalf("artist winner matching is wrong: %#v", got.Artist)
	}
}

func TestBuildDoesNotTrustOneTaggedFileInMultiTrackFolder(t *testing.T) {
	got := Build([]Evidence{{Artist: "Poison"}, {}, {}, {}})
	if got.Artist.Strong {
		t.Fatalf("one tag became authoritative: %#v", got.Artist)
	}
	if single := Build([]Evidence{{Artist: "Solo"}}); single.Artist.Strong {
		t.Fatalf("single-track release bypassed normal path/tag fusion: %#v", single.Artist)
	}
}

func TestBuildRejectsBelowEightyPercent(t *testing.T) {
	got := Build([]Evidence{
		{Artist: "A"}, {Artist: "A"}, {Artist: "A"},
		{Artist: "B"},
	})
	if got.Artist.Strong {
		t.Fatalf("75%% winner became authoritative: %#v", got.Artist)
	}
}

func TestArtistConsensusDoesNotCollapseWordPrefixNames(t *testing.T) {
	got := Build([]Evidence{{Artist: "DJ Paul"}, {Artist: "DJ Paul"}})
	if !got.Artist.Strong {
		t.Fatalf("expected DJ Paul consensus: %#v", got.Artist)
	}
	if got.Artist.Matches("DJ Paul Elstak") {
		t.Fatal("DJ Paul Elstak was treated as DJ Paul")
	}
}
