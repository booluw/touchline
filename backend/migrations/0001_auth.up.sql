-- =====================================================================
-- SCHEMA: auth
-- Minimal account/session layer. Kept separate from `manager` because a
-- user account and a manager identity are different concepts (an account
-- could exist before ever taking a job, or a person could hold multiple
-- non-manager roles in future phases — see person schema below).
-- =====================================================================
CREATE SCHEMA IF NOT EXISTS auth;

CREATE TABLE auth.users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    is_admin            BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ
);

CREATE TABLE auth.sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    refresh_token_hash  TEXT NOT NULL,
    device_fingerprint  TEXT,
    ip_address          INET,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ
);
CREATE INDEX idx_sessions_user ON auth.sessions(user_id);

