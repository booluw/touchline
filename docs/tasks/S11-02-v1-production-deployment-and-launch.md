# S11-02 — Finalize production Helm charts, k3s Contabo cluster, and V1 launch

**Status:** Not started  
**Sprint:** 11 — V1 integrity and launch review  
**Source:** technical plan §§15, 16; OPENCODE.md  
**Depends on:** S11-01

## What to do

Finalize production deployment manifests in `infra/helm` for deployment to a k3s cluster on Contabo infrastructure. Configure automated database migrations as pre-install/pre-upgrade Helm hooks. Set up OpenTelemetry tracing, Prometheus metrics collection, Grafana operational dashboards, and worker pod Horizontal Pod Autoscaling (HPA) based on event-bus queue depth. Execute final launch verification.

## Acceptance criteria

- Helm charts for `api`, `scheduler`, `worker`, and `frontend` deploy cleanly onto k3s Contabo production environment.
- `golang-migrate` pre-upgrade hooks execute database migrations safely against Neon Postgres before service deployment.
- Prometheus and Grafana dashboards monitor event-bus queue depth, API latency (P95/P99), DB connection pools, and WebSocket active connections.
- Worker pods scale automatically under heavy matchday event volume via HPA.
- System passes cold-start disaster recovery tests and automated backup/restore verification.

## Delivery evidence

- Pending.
