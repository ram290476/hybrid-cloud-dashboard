package bucket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSRoundTrip(t *testing.T) {
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "prefix="):
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"name":"prefs/user-1.json"}]}`))
		case r.Method == http.MethodGet:
			body, ok := objects["prefs/user-1.json"]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			objects["prefs/user-1.json"] = []byte(`{"user_sub":"user-1"}`)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	gcs := GCS{Host: server.URL, Name: "hcd-serving", Client: server.Client()}
	if err := gcs.Put("prefs/user-1.json", []byte(`{"user_sub":"user-1"}`)); err != nil {
		t.Fatal(err)
	}
	keys, err := gcs.List("prefs/")
	if err != nil || len(keys) != 1 {
		t.Fatalf("list %v %v", keys, err)
	}
	body, err := gcs.Get(keys[0])
	if err != nil || !strings.Contains(string(body), "user-1") {
		t.Fatalf("get %s %v", body, err)
	}
}

func TestS3SignsRequest(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("authorization")
		_, _ = w.Write([]byte(`<ListBucketResult><Contents><Key>prefs/user-1.json</Key></Contents></ListBucketResult>`))
	}))
	defer server.Close()
	s3 := S3{Endpoint: server.URL, BucketName: "hcd-serving", AccessKey: "minioadmin", SecretKey: "minioadmin", Client: server.Client()}
	keys, err := s3.List("prefs/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Fatalf("keys %v auth %s", keys, auth)
	}
}
