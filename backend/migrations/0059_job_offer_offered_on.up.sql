-- 0059: offers expire after 7 in-game days unanswered (daily world tick), so a
-- manager who never responds doesn't hold a club's one pending slot (0058)
-- forever. offered_on is the world's calendar date when the offer was made.
ALTER TABLE manager.job_offers ADD COLUMN offered_on DATE;
UPDATE manager.job_offers SET offered_on = world.world_date(world_id);
