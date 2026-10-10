-- 0062: team instructions on the club's tactical setup (IM61). Each dial is
-- -1 / 0 / +1 (0 = the neutral middle option), so existing rows play exactly
-- as before. Frozen into sim_inputs at kickoff like style/formation.
ALTER TABLE club.club_tactics
    ADD COLUMN mentality SMALLINT NOT NULL DEFAULT 0 CHECK (mentality BETWEEN -1 AND 1),
    ADD COLUMN pressing  SMALLINT NOT NULL DEFAULT 0 CHECK (pressing  BETWEEN -1 AND 1),
    ADD COLUMN width     SMALLINT NOT NULL DEFAULT 0 CHECK (width     BETWEEN -1 AND 1),
    ADD COLUMN tempo     SMALLINT NOT NULL DEFAULT 0 CHECK (tempo     BETWEEN -1 AND 1);
