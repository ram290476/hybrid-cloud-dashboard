package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/libs/merge"
	"github.com/ram290476/hybrid-cloud-dashboard/sync/internal/bucket"
	"github.com/ram290476/hybrid-cloud-dashboard/sync/internal/engine"
)

func main() {
	once := flag.Bool("once", false, "sync one pass and exit")
	interval := flag.Duration("interval", 30*time.Second, "poll interval")
	flag.Parse()
	pass := func() {
		replicas, err := load()
		if err != nil {
			log.Printf("load: %v", err)
			return
		}
		writes, err := engine.Step(replicas)
		if err != nil {
			log.Printf("merge: %v", err)
			return
		}
		if err := save(replicas); err != nil {
			log.Printf("save: %v", err)
			return
		}
		log.Printf("hcd-sync wrote %d replica documents", writes)
	}
	pass()
	if *once || os.Getenv("HCD_SYNC_ONCE") == "1" {
		return
	}
	ticker := time.NewTicker(*interval)
	for range ticker.C {
		pass()
	}
}

type loaded struct {
	engine.Replica
	bucket bucket.Bucket
	keys   map[string]string
}

func load() ([]engine.Replica, error) {
	loadedStores = nil
	stores := stores()
	out := make([]engine.Replica, 0, len(stores))
	for _, store := range stores {
		keys, err := store.bucket.List("prefs/")
		if err != nil {
			log.Printf("%s list: %v", store.Name, err)
			out = append(out, engine.Replica{Name: store.Name, Docs: map[string]merge.Doc{}})
			continue
		}
		docs := map[string]merge.Doc{}
		store.keys = map[string]string{}
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			body, err := store.bucket.Get(key)
			if err != nil {
				return nil, err
			}
			var doc merge.Doc
			if err := json.Unmarshal(body, &doc); err != nil {
				return nil, err
			}
			if doc.UserSub == "" {
				continue
			}
			docs[doc.UserSub] = doc
			store.keys[doc.UserSub] = key
		}
		store.Docs = docs
		out = append(out, store.Replica)
		loadedStores = append(loadedStores, store)
	}
	return out, nil
}

var loadedStores []loaded

func save(replicas []engine.Replica) error {
	byName := map[string]engine.Replica{}
	for _, replica := range replicas {
		byName[replica.Name] = replica
	}
	for _, store := range loadedStores {
		replica := byName[store.Name]
		for sub, doc := range replica.Docs {
			body, err := json.Marshal(doc)
			if err != nil {
				return err
			}
			key := store.keys[sub]
			if key == "" {
				key = "prefs/" + sub + ".json"
			}
			if err := store.bucket.Put(key, body); err != nil {
				return err
			}
		}
	}
	loadedStores = nil
	return nil
}

func stores() []loaded {
	// Endpoints come from the environment so the launchd plist and the kind
	// port-forwards share one binary. Empty endpoints are skipped.
	var out []loaded
	if host := os.Getenv("GCS_EMULATOR_HOST"); host != "" {
		out = append(out, loaded{Replica: engine.Replica{Name: "gcp-go"}, bucket: bucket.GCS{Host: host, Name: env("GCS_BUCKET", "hcd-serving")}})
	}
	if host := os.Getenv("S3_ENDPOINT"); host != "" {
		out = append(out, loaded{Replica: engine.Replica{Name: "aws-python"}, bucket: bucket.S3{
			Endpoint: host, BucketName: env("S3_BUCKET", "hcd-serving"),
			AccessKey: env("AWS_ACCESS_KEY_ID", "minioadmin"), SecretKey: env("AWS_SECRET_ACCESS_KEY", "minioadmin"),
		}})
	}
	if host := os.Getenv("AZURE_BLOB_ENDPOINT"); host != "" {
		out = append(out, loaded{Replica: engine.Replica{Name: "azure-dotnet"}, bucket: bucket.Azure{
			Endpoint: host, Account: env("AZURE_STORAGE_ACCOUNT", "devstoreaccount1"),
			Key: env("AZURE_STORAGE_KEY", azuriteKey), Container: env("BLOB_CONTAINER", "hcd-serving"),
		}})
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// azuriteKey is the public Azurite well-known key from Microsoft docs. It is not a cloud credential.
const azuriteKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
