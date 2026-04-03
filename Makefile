# Paths
CMD := ./cmd/api
BINARY := weather-api
BINDIR := bin

# Release cross-compile (override for your server, e.g. GOOS=darwin GOARCH=arm64)
GOOS ?= linux
GOARCH ?= amd64

# Container image for docker-build / push to your registry before Kubernetes deploy
IMAGE ?= weather-api:local

# Image tag must match deploy/k8s/overlays/local/patch-deployment.yaml
K8S_LOCAL_IMAGE ?= weather-api:local
# If non-empty: always run `kind load docker-image ... --name $(KIND_CLUSTER)` (see ErrImageNeverPull in deploy/k8s/README.md)
KIND_CLUSTER ?=

# Avoid server OpenAPI download (often flakes or fails if API is still starting / kubeconfig port is stale).
# Set KUBECTL_APPLY_FLAGS= to restore default validation.
KUBECTL_APPLY_FLAGS ?= --validate=false

.PHONY: run build test deploy clean docker-build \
	k8s-local-check k8s-local-build k8s-local-maybe-load k8s-local-apply k8s-local-wait k8s-local-smoke k8s-local-up k8s-local-down k8s-local-debug

## run: start the API locally (listens on :8080)
run:
	go run $(CMD)

## build: compile to bin/$(BINARY) for the current OS/arch
build: $(BINDIR)/$(BINARY)

$(BINDIR)/$(BINARY):
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/$(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## deploy: produce a stripped release binary for $(GOOS)/$(GOARCH) at bin/$(BINARY)-$(GOOS)-$(GOARCH)
deploy:
	mkdir -p $(BINDIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags="-s -w" \
		-o $(BINDIR)/$(BINARY)-$(GOOS)-$(GOARCH) $(CMD)
	@echo "Artifact: $(BINDIR)/$(BINARY)-$(GOOS)-$(GOARCH)"

## clean: remove build outputs
clean: clean-bindir

clean-bindir:
	rm -rf $(BINDIR)

## docker-build: OCI image (used by deploy/k8s Deployment; set IMAGE=registry/repo:tag)
docker-build:
	docker build -t $(IMAGE) .

## k8s-local-* : local Kubernetes (Docker Desktop OR minikube) — build image, apply overlay, smoke-test
k8s-local-check:
	@ctx=$$(kubectl config current-context 2>/dev/null || echo '(none)'); \
	printf 'Checking kubectl context: %s\n' "$$ctx"; \
	if kubectl cluster-info >/dev/null 2>&1; then exit 0; fi; \
	echo ""; \
	echo "kubectl cannot reach the Kubernetes API (connection refused / timeout)."; \
	case "$$ctx" in \
	  *minikube*) \
	    echo "You are on minikube. The cluster is probably stopped. Run:"; \
	    echo "  minikube start"; \
	    echo "Then: make k8s-local-up   (builds, runs minikube image load, applies manifests)"; \
	    echo "Or switch to Docker Desktop K8s: kubectl config use-context docker-desktop"; \
	    ;; \
	  *docker-desktop*) \
	    echo "Docker Desktop → Settings → Kubernetes → enable Kubernetes, wait until Running."; \
	    echo "Or: Troubleshoot → Restart Docker Desktop."; \
	    ;; \
	  *) \
	    echo "Start the cluster for this context, or pick another context:"; \
	    echo "  kubectl config get-contexts"; \
	    echo "  kubectl config use-context <name>"; \
	    ;; \
	esac; \
	echo ""; \
	echo "Confirm with: kubectl cluster-info"; \
	exit 1

k8s-local-build:
	docker build -t $(K8S_LOCAL_IMAGE) .

# Load image into the cluster (minikube / kind). imagePullPolicy:Never needs the image on the node.
# Docker Desktop’s Kubernetes often does NOT see `docker build` images → ErrImageNeverPull → use kind load.
k8s-local-maybe-load:
	@ctx=$$(kubectl config current-context 2>/dev/null || true); \
	case "$$ctx" in \
	  *minikube*) \
	    echo "Loading $(K8S_LOCAL_IMAGE) into minikube..."; \
	    minikube image load $(K8S_LOCAL_IMAGE); \
	    ;; \
	esac; \
	if command -v kind >/dev/null 2>&1; then \
	  if [ -n "$(KIND_CLUSTER)" ]; then \
	    echo "kind load docker-image $(K8S_LOCAL_IMAGE) --name $(KIND_CLUSTER)"; \
	    kind load docker-image $(K8S_LOCAL_IMAGE) --name "$(KIND_CLUSTER)"; \
	  elif echo "$$ctx" | grep -q '^kind-'; then \
	    cluster=$$(echo "$$ctx" | sed 's/^kind-//'); \
	    echo "kind load docker-image $(K8S_LOCAL_IMAGE) --name $$cluster"; \
	    kind load docker-image $(K8S_LOCAL_IMAGE) --name "$$cluster"; \
	  else \
	    n=$$(kind get clusters 2>/dev/null | wc -l | tr -d ' '); \
	    if [ "$$n" -eq 1 ]; then \
	      c=$$(kind get clusters 2>/dev/null | head -1); \
	      echo "kind load docker-image $(K8S_LOCAL_IMAGE) --name $$c"; \
	      kind load docker-image $(K8S_LOCAL_IMAGE) --name "$$c"; \
	    elif [ "$$n" -gt 1 ]; then \
	      echo "Multiple kind clusters — pick one:"; kind get clusters; \
	      echo "Then: make k8s-local-maybe-load KIND_CLUSTER=<name>"; \
	    else \
	      echo "kind is installed but 'kind get clusters' is empty — create a cluster or use minikube load."; \
	    fi; \
	  fi; \
	elif ! echo "$$ctx" | grep -q minikube; then \
	  echo "Tip: ErrImageNeverPull? Install kind (https://kind.sigs.k8s.io/) then:"; \
	  echo "  kind load docker-image $(K8S_LOCAL_IMAGE)   # default cluster name is usually 'kind'"; \
	  echo "  make k8s-local-maybe-load KIND_CLUSTER=kind"; \
	fi

k8s-local-apply: k8s-local-check
	kubectl apply $(KUBECTL_APPLY_FLAGS) -k deploy/k8s/overlays/local

k8s-local-wait: k8s-local-check
	kubectl -n weather-api rollout status deployment/weather-api --timeout=180s

k8s-local-smoke: k8s-local-check
	kubectl -n weather-api run k8s-local-curl-smoke --rm --attach --restart=Never \
		--image=curlimages/curl:8.7.1 -- curl -sf http://weather-api/health && echo "k8s-local-smoke: OK"

k8s-local-up: k8s-local-check k8s-local-build k8s-local-maybe-load k8s-local-apply k8s-local-wait k8s-local-smoke

k8s-local-down: k8s-local-check
	kubectl delete $(KUBECTL_APPLY_FLAGS) -k deploy/k8s/overlays/local --ignore-not-found

## k8s-local-debug: print pod status + events + logs (when rollout hangs)
k8s-local-debug:
	@kubectl -n weather-api get pods -l app=weather-api -o wide 2>/dev/null || true
	@echo "--- describe ---"
	@kubectl -n weather-api describe pod -l app=weather-api 2>/dev/null | tail -80 || true
	@echo "--- logs (current) ---"
	@kubectl -n weather-api logs -l app=weather-api --tail=80 --all-containers=true 2>/dev/null || true
