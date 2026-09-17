// Event constants of the player lifecycle (A06).
package lifecycle

// EventPlayerRetired fires when a player retires at seasonal rollover.
const EventPlayerRetired = "PLAYER_RETIRED"

// EventWorldLifecycleCompleted fires once per (world, season) after the
// full rollover sequence (intake → retirement → replenish) has run.
const EventWorldLifecycleCompleted = "WORLD_LIFECYCLE_SEASON_COMPLETED"

// EventAIAutoFill fires when the seasonal rollover signs free agents into an
// AI-controlled club's thin squad (A09).
const EventAIAutoFill = "AI_AUTO_FILL"
