# STORY-01 — Event Pattern Evaluator, Story Scaffolding & DB Schema

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/storyline-numerics.md`  
**Depends on:** S01-01 (migrations), S01-02 (event log)  

---

## 1. Goal

Establish the database schema (`story.arcs`, `story.dilemmas`, `story.chronicles`) and backend scaffolding (`internal/storyline`) for evaluating world event streams against narrative arc trigger patterns during world ticks.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Database Schema Migration
Create numbered migration `NNNN_storyline_schema.up.sql` and down migration:
- Table `story.arcs`:
  - `id` UUID PRIMARY KEY DEFAULT gen_random_uuid()
  - `world_id` UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE
  - `club_id` UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE
  - `manager_id` UUID REFERENCES manager.managers(id) ON DELETE SET NULL
  - `arc_type` VARCHAR(64) NOT NULL
  - `state` VARCHAR(32) NOT NULL DEFAULT 'triggered' -- triggered | escalating | climax | resolved
  - `context_json` JSONB NOT NULL DEFAULT '{}'
  - `created_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()
  - `updated_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()
  - Indexes: `(world_id, club_id, state)`, `(world_id, arc_type, created_at)`
- Table `story.dilemmas`:
  - `id` UUID PRIMARY KEY DEFAULT gen_random_uuid()
  - `arc_id` UUID NOT NULL REFERENCES story.arcs(id) ON DELETE CASCADE
  - `world_id` UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE
  - `manager_id` UUID NOT NULL REFERENCES manager.managers(id) ON DELETE CASCADE
  - `title` VARCHAR(255) NOT NULL
  - `description` TEXT NOT NULL
  - `options_json` JSONB NOT NULL
  - `status` VARCHAR(32) NOT NULL DEFAULT 'pending' -- pending | resolved | expired
  - `chosen_option_id` VARCHAR(64)
  - `expires_at_tick` BIGINT NOT NULL
  - `created_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()
  - `resolved_at` TIMESTAMPTZ
  - Indexes: `(manager_id, status)`, `(world_id, expires_at_tick)`
- Table `story.chronicles`:
  - `id` UUID PRIMARY KEY DEFAULT gen_random_uuid()
  - `world_id` UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE
  - `season_number` INT NOT NULL
  - `club_id` UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE
  - `manager_id` UUID REFERENCES manager.managers(id) ON DELETE SET NULL
  - `headline` VARCHAR(255) NOT NULL
  - `summary_json` JSONB NOT NULL
  - `created_at` TIMESTAMPTZ NOT NULL DEFAULT NOW()
  - UNIQUE `(world_id, season_number, club_id)`

Add migration row entry to `backend/migrations/README.md`.

### 2.2 Domain Models (`backend/internal/storyline/model.go`)
- Define Go structs: `StoryArc`, `StoryDilemma`, `DilemmaOption`, `NarrativeConsequence`, `SeasonChronicle`.
- Define constants: `MaxActiveStoryArcsPerClub = 2`, `StoryArcCooldownDays = 30`, `DilemmaExpirationTicks = 7`.

### 2.3 Evaluator Scaffolding (`backend/internal/storyline/evaluator.go`)
- Create `Evaluator` struct taking `(store Store, eventBus EventPublisher)`.
- Implement `EvaluateWorldTick(ctx context.Context, worldID string, tick int64) error`:
  - Load active arcs per club in the world.
  - Skip clubs already reaching `MaxActiveStoryArcsPerClub`.
  - Check dormant patterns against recent `world.events` and club state snapshots.
  - Advance escalating arcs.

### 2.4 Event Spine Integration (`backend/internal/storyline/events.go`)
- Emit `STORY_ARC_TRIGGERED`, `STORY_ARC_ESCALATED`, `STORY_ARC_RESOLVED`, `STORY_DILEMMA_CREATED`, `STORY_DILEMMA_RESOLVED`.
- Every event includes typed JSON `Explanation` details.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/storyline/evaluator_test.go` verifying arc state machine transitions and deduplication.
- Verify migration `up.sql` and `down.sql` execute cleanly against Postgres 16.

### 3.2 Verification Commands
```bash
gofmt -w internal/storyline/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/storyline/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] DB Migration created & added to `migrations/README.md`
- [ ] Go models in `internal/storyline/model.go`
- [ ] Evaluator runner in `internal/storyline/evaluator.go`
- [ ] Passing unit and integration tests
