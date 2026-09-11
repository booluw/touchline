# S14-01 — Extract Match and Transfer engines into standalone gRPC microservices

**Status:** Not started  
**Sprint:** 14 — V2 infrastructure and world expansion  
**Source:** technical plan §§3, 16; OPENCODE.md  
**Depends on:** S11-02

## What to do

Extract the high-throughput Match Simulation engine and Transfer engine into standalone microservices communicating over gRPC. Leverage the modular monolith architecture established in Phase 0 where engine interfaces (`MatchService`, `TransferService`) are preserved, replacing in-process Go method calls with gRPC client transports without rewriting core domain logic.

## Acceptance criteria

- `MatchService` and `TransferService` Protobuf schemas define complete RPC contracts for simulation and transfer workflows.
- Standalone binaries for match engine and transfer engine deploy independently in Kubernetes/k3s.
- In-process Go interface implementations can switch seamlessly between local execution and gRPC client transport via environment configuration.
- End-to-end match simulation performance under load achieves high throughput with zero domain regression.
- Independent Horizontal Pod Autoscaling (HPA) policies target gRPC service load independently from API pods.

## Delivery evidence

- Pending.
