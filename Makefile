.PHONY: test test-go test-python test-dotnet test-node demo dev-token contract-test terraform-validate cluster-up cluster-down local-seed pf-emulators sync-status lint

export PATH := $(HOME)/.dotnet:$(PATH)
DOTNET ?= dotnet

test: test-go test-python test-dotnet test-node contract-test

test-go:
	cd libs/merge && go test ./...
	cd facade && go test ./...
	cd backends/go-gcp && go test ./...
	cd sync && go test ./...

test-python:
	python3 -m pytest backends/python-aws/tests -q

test-dotnet:
	$(DOTNET) test backends/dotnet-azure/tests/DashboardApi.Tests/DashboardApi.Tests.csproj --nologo

test-node:
	node --test frontend/*.test.js

dev-token:
	python3 scripts/dev_token.py

contract-test:
	python3 scripts/contract-test.py

demo:
	bash scripts/demo.sh

local-seed:
	python3 scripts/local-seed.py

sync-status:
	cd sync && HCD_SYNC_ONCE=1 go run ./cmd/hcd-sync -once

terraform-validate:
	@set -e; for dir in infra/terraform/gcp infra/terraform/aws infra/terraform/azure infra/terraform/cloudflare infra/terraform/aws/modules/lambda-url infra/terraform/labs/eks infra/terraform/labs/aks infra/terraform/labs/gke; do \
	  echo "== $$dir"; \
	  terraform -chdir=$$dir init -backend=false -input=false -lockfile=readonly >/tmp/hcd-tf-init.log; \
	  terraform -chdir=$$dir validate; \
	done

cluster-up:
	@if ! docker inspect kind-registry >/dev/null 2>&1; then \
	  docker run -d --name kind-registry --restart=always -p 127.0.0.1:5001:5000 registry:2; \
	else \
	  docker start kind-registry >/dev/null; \
	fi
	@if kind get clusters 2>/dev/null | grep -qx hcd; then \
	  echo "kind cluster hcd already exists; leaving it in place"; \
	else \
	  kind create cluster --name hcd --config deploy/local/kind-config.yaml; \
	fi
	@docker network connect kind kind-registry >/dev/null 2>&1 || true
	@for node in $$(kind get nodes --name hcd); do \
	  docker exec "$$node" mkdir -p /etc/containerd/certs.d/localhost:5001; \
	  printf '%s\n' '[host."http://kind-registry:5000"]' | docker exec -i "$$node" cp /dev/stdin /etc/containerd/certs.d/localhost:5001/hosts.toml; \
	done
	helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm --namespace envoy-gateway-system --create-namespace
	kubectl apply -k deploy/overlays/local

cluster-down:
	kind delete cluster --name hcd || true
	docker rm -f kind-registry || true

pf-emulators:
	kubectl -n hcd-deps port-forward svc/minio 9000:9000 &
	kubectl -n hcd-deps port-forward svc/fake-gcs 4443:4443 &
	kubectl -n hcd-deps port-forward svc/azurite 10000:10000 &
	kubectl -n hcd-deps port-forward svc/dynamodb 8000:8000 &
	kubectl -n hcd-deps port-forward svc/firestore 8787:8080 &
	kubectl -n hcd-deps port-forward svc/cosmos 18081:8081 18080:8080 11234:1234 &

lint:
	gofmt -w libs facade backends/go-gcp sync
	python3 -m compileall -q backends/python-aws/src
