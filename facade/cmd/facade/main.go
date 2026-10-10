package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ram290476/hybrid-cloud-dashboard/facade/policy"
	"github.com/ram290476/hybrid-cloud-dashboard/facade/proxy"
)

func main() {
	path := env("ROUTING_POLICY", "facade/policy/routing-policy.local.yaml")
	doc, hash, err := policy.LoadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	server := proxy.New(doc, hash)
	server.FacadeKey = os.Getenv("FACADE_KEY")
	server.DevSecret = env("DEV_JWT_SECRET", "local-dev-secret-change-me")
	server.Issuer = env("DEV_JWT_ISSUER", "http://localhost/dev-auth")
	server.Audience = env("DEV_JWT_AUDIENCE", "hcd-local")
	server.DevInject = os.Getenv("DEV_INJECT_TOKEN") == "1"
	server.Frontend = os.Getenv("FRONTEND_DIR")
	server.Salt = env("AUDIT_IP_SALT", "hcd-local-salt")
	if v := os.Getenv("AUDIT_IP_SALT"); v != "" {
		server.Salt = v
	}
	go func() {
		interval := time.Duration(doc.Spec.Evaluation.IntervalS) * time.Second
		if interval <= 0 {
			interval = 5 * time.Second
		}
		ticker := time.NewTicker(interval)
		server.ProbeOnce()
		for range ticker.C {
			server.ProbeOnce()
		}
	}()
	addr := ":" + env("PORT", "8080")
	log.Printf("facade listening on %s policy %s hash %s", addr, doc.Metadata.Version, hash)
	log.Fatal(http.ListenAndServe(addr, server))
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
