# Kubernetes deployment

Layouts:

- **`base/`** — shared manifests (namespace, ConfigMap, Deployment, Service).
- **Root `kustomization.yaml`** — `kubectl apply -k deploy/k8s` applies **base** only (registry-style image).
- **`overlays/local/`** — local cluster: image **`weather-api:local`**, **`imagePullPolicy: Never`**, **1** replica.

## What you get

- **Namespace** `weather-api`
- **Deployment** — `/health` liveness + readiness, non-root user **65532**
- **Service** — `ClusterIP` port **80** → container **8080**
- **ConfigMap** — `LISTEN_ADDR`, `NWS_USER_AGENT` (edit before prod)

Secrets (`WEATHER_API_KEY`) are optional; see below.

---

## Test locally

Use **`kubectl config current-context`** so it matches the cluster you intend (`docker-desktop` vs `minikube`).

### Docker Desktop Kubernetes

1. **Settings → Kubernetes** — enable and wait until **Running**.
2. `kubectl cluster-info` must succeed.
3. **Important:** many setups use a **separate containerd** inside the Kubernetes VM/node. **`docker build -t weather-api:local` on the Mac does not automatically make that image visible to kubelet**, so you often see **`ErrImageNeverPull`** with **`imagePullPolicy: Never`**.

**Fix (pick one):**

- **If you use [kind](https://kind.sigs.k8s.io/)** (or Docker Desktop’s cluster behaves like kind), load the image after every build:
  ```bash
  docker build -t weather-api:local .
  kind load docker-image weather-api:local --name kind
  ```
  The default kind cluster name is usually **`kind`**. Named clusters: `kind load docker-image weather-api:local --name <cluster>`.
- **Makefile:** `make k8s-local-maybe-load` runs **`kind load`** when the `kind` CLI exists and either your context is `kind-<name>` or exactly **one** kind cluster is listed by `kind get clusters`. Override with **`KIND_CLUSTER=name`**.
- **Minikube:** use **`minikube image load weather-api:local`** (wired in `make k8s-local-up` when the context contains `minikube`).

Then re-create the pod:

```bash
kubectl delete pod -n weather-api -l app=weather-api --wait=false
kubectl -n weather-api rollout status deployment/weather-api --timeout=180s
```

### Minikube

1. **Start the VM / cluster** (this is why you see `connection refused` when it is stopped):
   ```bash
   minikube start
   ```
2. `kubectl cluster-info` — should work with context `minikube`.
3. `make k8s-local-up` — the Makefile runs **`minikube image load weather-api:local`** when the context name contains `minikube`, so the cluster can run **`imagePullPolicy: Never`** without a registry.

If your minikube context is renamed and no longer contains the substring `minikube`, run manually after `docker build`:
```bash
minikube image load weather-api:local
```

### Troubleshooting `connection refused`

`kubectl` uses the `server:` URL in `~/.kube/config` (often `https://127.0.0.1:<random-port>`). **Connection refused** means the API server is not listening — usually the **cluster is off**, not a bug in the manifests.

| Context | What to do |
|--------|------------|
| **`minikube`** | `minikube start` — then `kubectl cluster-info` |
| **`docker-desktop`** | Enable Kubernetes in Docker Desktop; wait until Running |
| **Wrong cluster** | `kubectl config get-contexts` → `kubectl config use-context <name>` |

The Makefile uses **`--validate=false`** on apply/delete by default (OpenAPI fetch flake). Unreachable API is fixed by starting the right cluster, not by flags.

```bash
# After cluster is up: build, (minikube load if needed), apply, rollout, smoke test
make k8s-local-up
```

Then either:

```bash
kubectl -n weather-api port-forward svc/weather-api 8080:80
curl -s http://127.0.0.1:8080/health
```

Or run only the in-cluster smoke test:

```bash
make k8s-local-smoke
```

Tear down:

```bash
make k8s-local-down
```

### Rollout stuck / `timed out waiting for the condition`

Yes — **`make k8s-local-up` is meant for a local cluster** (Docker Desktop or minikube), not a cloud control plane.

1. **`1 old replicas are pending termination`** — Docker Desktop sometimes leaves the **previous** pod in `Terminating` during a **rolling** update. The **local overlay** uses **`strategy: Recreate`** so only one ReplicaSet generation runs at a time (re-apply overlay after `git pull`):
   ```bash
   kubectl apply --validate=false -k deploy/k8s/overlays/local
   ```
   If a pod is stuck forever in `Terminating`, force-remove (last resort):
   ```bash
   kubectl -n weather-api get pods
   kubectl -n weather-api delete pod <stuck-pod-name> --force --grace-period=0
   ```

2. **Pods not Ready** — readiness returning non-200 (e.g. old images still **rate-limiting `/health`**). **Rebuild** `weather-api:local` and restart the Deployment.

3. **Inspect:**
   ```bash
   kubectl -n weather-api get pods -o wide
   kubectl -n weather-api describe pod -l app=weather-api
   kubectl -n weather-api logs -l app=weather-api --tail=50
   kubectl -n weather-api get events --sort-by=.lastTimestamp | tail -20
   ```

**Why `imagePullPolicy: Never`?** The overlay expects the image **on the node**. Docker Desktop K8s shares the host Docker daemon; **minikube** needs **`minikube image load`** (wired in `make k8s-local-up` when the context name contains `minikube`). For **kind** / **k3d**, use their `load image` commands.

---

## Build and push (remote / production)

```bash
docker build -t <your-registry>/weather-api:<tag> .
docker push <your-registry>/weather-api:<tag>
```

Edit **`deploy/k8s/base/deployment.yaml`** `image:` (or use your own overlay).

```bash
kubectl apply -k deploy/k8s
# or explicitly:
kubectl apply -k deploy/k8s/base
```

## Optional: Weather Company API key

```bash
kubectl -n weather-api create secret generic weather-api-secrets \
  --from-literal=WEATHER_API_KEY='your-key' \
  --dry-run=client -o yaml | kubectl apply -f -
```

The Deployment uses **`optional: true`** on that secret so pods start without it.

## Optional: Ingress

Example: **`ingress.yaml`** in this directory (not applied by default). Apply manually or add it under `resources:` in `base/kustomization.yaml`. Adjust `host` and `ingressClassName`.

## Production notes

- Pin **`image:`** to a digest or immutable tag.
- Tune **resources** and **replicas** from metrics.
- **Prometheus**: scrape `/metrics`; you may want a `PodMonitor` / annotations depending on your stack.
- **Rate limiting** uses `RemoteAddr`; behind a proxy, consider **X-Forwarded-For** if clients hit the ingress directly.
