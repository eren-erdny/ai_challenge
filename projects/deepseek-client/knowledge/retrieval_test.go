package knowledge

import (
	"math"
	"testing"
)

func TestRetrievalPersistenceIsolationAndInvalidSettings(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Dir: dir}
	baseline := RetrievalOptions{Candidates: 30, TopK: 3}
	if err := m.SetRetrieval("one", baseline); err != nil {
		t.Fatal(err)
	}
	reopened := &Manager{Dir: dir}
	got, err := reopened.Retrieval("one")
	if err != nil || got.Candidates != 30 || got.TopK != 3 || got.Rerank || got.Rewrite || got.MinSimilarity != nil {
		t.Fatalf("%+v %v", got, err)
	}
	other, err := reopened.Retrieval("two")
	if err != nil || !other.Rerank || !other.Rewrite || other.MinSimilarity == nil {
		t.Fatalf("%+v %v", other, err)
	}
	nan := math.NaN()
	bad := baseline
	bad.MinSimilarity = &nan
	if reopened.SetRetrieval("one", bad) == nil {
		t.Fatal("accepted NaN")
	}
	bad = baseline
	bad.TopK = 31
	if reopened.SetRetrieval("one", bad) == nil {
		t.Fatal("accepted top-K greater than candidates")
	}
	got, _ = reopened.Retrieval("one")
	if got.TopK != 3 {
		t.Fatal("failed update replaced settings")
	}
}
