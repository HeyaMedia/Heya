package matcher

import (
	"errors"
	"reflect"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

func TestCollectRichPersonCreditsUsesOneCastAndCrewOrder(t *testing.T) {
	t.Parallel()

	personA := "00000000-0000-0000-0000-000000000001"
	personB := "00000000-0000-0000-0000-000000000002"
	detail := &metadata.MediaDetail{
		Cast: []metadata.CastMember{
			{Name: "Person B", Character: "Lead", CanonicalID: personB},
			{Name: "Person A", Character: "Cameo", CanonicalID: personA},
		},
		Crew: []metadata.CrewMember{
			{Name: "Person B", Job: "Director", CanonicalID: personB},
			{Name: "Person A", Job: "Writer", CanonicalID: personA},
		},
	}

	credits := collectRichPersonCredits(detail)
	got := make([]string, len(credits))
	for i, credit := range credits {
		got[i] = richPersonCreditKey(credit)
	}
	want := []string{
		personA + "|cast|Cameo|Person A",
		personA + "|crew||Writer|Person A",
		personB + "|cast|Lead|Person B",
		personB + "|crew||Director|Person B",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("combined person order = %#v, want %#v", got, want)
	}
}

func TestSortResolvedPersonCreditsUsesDatabaseLockKey(t *testing.T) {
	t.Parallel()

	resolved := []resolvedPersonCredit{
		{person: sqlc.Person{ID: 42}, credit: richPersonCredit{name: "First in payload"}},
		{person: sqlc.Person{ID: 7}, credit: richPersonCredit{name: "Second in payload"}},
		{person: sqlc.Person{ID: 42}, credit: richPersonCredit{name: "Another role", isCast: true}},
	}
	sortResolvedPersonCredits(resolved)

	got := []int64{resolved[0].person.ID, resolved[1].person.ID, resolved[2].person.ID}
	want := []int64{7, 42, 42}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("person lock order = %v, want %v", got, want)
	}
}

func TestRichFailureStopsTransactionFanout(t *testing.T) {
	t.Parallel()

	testErr := errors.New("database statement failed")
	for _, tc := range []struct {
		name string
		inTx bool
		stop bool
	}{
		{name: "pool continues", stop: false},
		{name: "transaction stops", inTx: true, stop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got richErrs
			matcher := &Matcher{inTx: tc.inTx}
			if stop := matcher.richFailure(&got, testErr); stop != tc.stop {
				t.Fatalf("richFailure stop = %v, want %v", stop, tc.stop)
			}
			if !errors.Is(got.result(), testErr) {
				t.Fatalf("richFailure did not retain original error: %v", got.result())
			}
		})
	}
}
