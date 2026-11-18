package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGCSListObjectsPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"items":[{"name":"serving/chart/TSLA.json","updated":"2026-10-09T22:00:00.000Z"}],"nextPageToken":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"name":"serving/stock/TSLA.json","updated":"2026-10-09T21:00:00Z"}]}`))
	}))
	defer srv.Close()
	got, err := (GCS{Host: srv.URL, Bucket: "hcd-serving"}).ListObjects(context.Background(), "serving/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "serving/chart/TSLA.json" || got[1].Key != "serving/stock/TSLA.json" || got[0].Modified.IsZero() {
		t.Fatalf("%#v", got)
	}
}

func TestGCSReadyCreatesMissingBucket(t *testing.T) {
	var created atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/b/hcd-serving":
			if !created.Load() {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/b":
			if r.URL.Query().Get("project") != "hcd-local" {
				t.Errorf("project = %q", r.URL.Query().Get("project"))
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"name":"hcd-serving"}` {
				t.Errorf("body = %s", body)
			}
			created.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := GCS{Host: srv.URL, Bucket: "hcd-serving"}
	if err := g.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !created.Load() {
		t.Fatal("expected bucket create")
	}
	if err := g.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}
