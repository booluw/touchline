-- The cup bracket read-back is campaign-scoped; dropping it on rollback is
-- safe because cup progress is regenerated lazily by the competition service.
DROP TABLE IF EXISTS competition.cup_bracket;