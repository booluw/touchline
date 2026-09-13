DROP TABLE IF EXISTS club.club_training_plans;
DROP TABLE IF EXISTS club.club_tactics;

DELETE FROM player.player_attributes
WHERE attribute_key IN ('tackling', 'marking', 'teamwork');