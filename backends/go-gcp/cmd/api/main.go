package main

import (
	"log"
	"net/http"
	"os"

	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/api"
	"github.com/ram290476/hybrid-cloud-dashboard/backends/go-gcp/internal/store"
)

func main() {
	mem := store.NewMemory()
	seed(mem, env("FIXTURES_DIR", "contract/fixtures"))
	var st store.Store = mem
	if host := os.Getenv("STORAGE_EMULATOR_HOST"); host != "" {
		gcs := store.GCS{Host: host, Bucket: env("GCS_BUCKET", "hcd-serving")}
		st = store.Pair{Objects: gcs, Prefs: gcs}
		if fire := os.Getenv("FIRESTORE_EMULATOR_HOST"); fire != "" {
			_ = fire // Firestore emulator is probed by readyz in a later client; prefs stay on the paired store.
		}
	}
	srv := &api.Server{
		Store: st, Backend: env("APP_BACKEND", "gcp-go"), Lang: env("APP_LANG", "go"),
		Runtime: env("APP_RUNTIME", "cloud-run"), Color: env("APP_COLOR", "blue"),
		Version: env("APP_VERSION", "dev"), DataVersion: env("DATA_VERSION", "fixture-2026.10.09"),
		AuthMode: env("AUTH_MODE", "dev"), DevSecret: env("DEV_JWT_SECRET", "local-dev-secret-change-me"),
		Issuer: env("DEV_JWT_ISSUER", "http://localhost/dev-auth"), Audience: env("DEV_JWT_AUDIENCE", "hcd-local"),
		FacadeKey: os.Getenv("FACADE_KEY"),
	}
	addr := ":" + env("PORT", "8080")
	log.Printf("go-gcp listening on %s color %s", addr, srv.Color)
	log.Fatal(http.ListenAndServe(addr, srv))
}

func seed(mem *store.Memory, dir string) {
	for _, key := range []string{"serving/dashboard.json", "serving/status.json", "serving/chart/TSLA.json", "serving/chart/SPCX.json"} {
		body, err := os.ReadFile(dir + "/" + key)
		if err != nil {
			continue
		}
		_ = mem.PutObject(nil, key, body)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
