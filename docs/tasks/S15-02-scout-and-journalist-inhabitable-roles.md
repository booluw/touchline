# S15-02 — Implement specialized gameplay interfaces for Scout and Journalist roles

**Status:** Not started  
**Sprint:** 15 — Multi-role universe foundation  
**Source:** PRD §77; technical plan §16; OPENCODE.md  
**Depends on:** S09-03, S15-01

## What to do

Build dedicated Nuxt gameplay interfaces and backend command handlers for Scout and Journalist roles. Scouts perform assignment scouting, write talent reports, and sell player intelligence to clubs. Journalists investigate club dynamics, publish articles/rumors directly to the media read-model, conduct manager interviews, and influence public opinion.

## Acceptance criteria

- Scout workspace allows building assignment networks, evaluating hidden player traits, and selling scouting dossier reports to managers.
- Journalist workspace allows drafting media stories, submitting press questions to managers, and publishing newsfeed stories.
- Journalist publications influence supporter sentiment, board pressure, and transfer rumor momentum.
- Manager responses to press questions update public relationship edges in `social.relationships`.
- All scout and journalist actions generate auditable events in `world.events`.

## Delivery evidence

- Pending.
