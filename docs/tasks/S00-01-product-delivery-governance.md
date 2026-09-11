# S00-01 — Establish product delivery governance

**Status:** Done  
**Sprint:** 00 — Product control plane  
**Source:** PRD §§74–85; technical plan §§16–18  
**Depends on:** None

## What to do

Use this backlog as the delivery record. Assign an owner, target sprint, and delivery evidence to every task before work begins. Preserve phase boundaries and maintain the open-decision log in `docs/product_manager.md`.

## Acceptance criteria

- Every task under `docs/tasks/` has an owner, status, and implementation or verification evidence before it is marked Done.
- Work entering a sprint is within the source-defined scope for that phase.
- Any requirement not resolved by the PRD, technical plan, or `OPENCODE.md` is recorded as an open decision before implementation relies on it.
- MVP advancement is evaluated against the PRD §85 core-loop question and the product metrics in PRD §79.

## Delivery evidence

- Governance conventions recorded in `docs/tasks/README.md` (Working rules): ownership is assigned per task only when implementation is requested; Done requires demonstrably met acceptance criteria with linked evidence; work entering a sprint must match phase/sprint scope; unresolved requirements go to the OPD registry in `docs/product_manager.md`; MVP validation references PRD §85 and §79.
- Owner/status/evidence tracking lives in each task file's header and `Delivery evidence` section. No task ownership was bulk-assigned; ownership is set when a task is requested for implementation (see Working rules).
