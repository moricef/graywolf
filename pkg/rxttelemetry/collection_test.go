package rxttelemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCollectionConsumesAndCombinesIndependentSources(t *testing.T) {
	server := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
	}
	one := server(`[{"packet":"A>B:first","rxt_hops":[{"from":"A","to":"B","has_data":true,"rssi_dbm":-100}]}]`)
	defer one.Close()
	two := server(`[{"packet":"C>D:second","rxt_hops":[{"from":"C","to":"D","has_data":true,"rssi_dbm":-110}]}]`)
	defer two.Close()

	collection := NewCollection([]SourceConfig{{Endpoint: one.URL}, {Endpoint: two.URL}}, nil, nil)
	collection.sources[one.URL].pollOnce(context.Background())
	collection.sources[two.URL].pollOnce(context.Background())

	if got := collection.Endpoints(); len(got) != 2 {
		t.Fatalf("endpoints=%v", got)
	}
	if got := collection.Statuses(time.Now().UTC()); len(got) != 2 {
		t.Fatalf("statuses=%+v", got)
	}
	links := collection.Snapshot(time.Now().UTC())
	if len(links) != 2 {
		t.Fatalf("combined links=%+v", links)
	}
}

func TestCollectionCanAddAndRemoveSources(t *testing.T) {
	collection := NewCollection([]SourceConfig{{Endpoint: "http://one.invalid/rxt.json"}}, nil, nil)
	collection.SetSources([]SourceConfig{
		{Endpoint: "http://one.invalid/rxt.json"},
		{Endpoint: "http://two.invalid/rxt.json"},
	})
	if got := collection.Endpoints(); len(got) != 2 {
		t.Fatalf("after add=%v", got)
	}
	collection.SetSources([]SourceConfig{{Endpoint: "http://two.invalid/rxt.json"}})
	if got := collection.Endpoints(); len(got) != 1 || got[0] != "http://two.invalid/rxt.json" {
		t.Fatalf("after remove=%v", got)
	}
}
