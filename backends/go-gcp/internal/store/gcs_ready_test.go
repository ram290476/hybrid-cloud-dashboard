package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

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
