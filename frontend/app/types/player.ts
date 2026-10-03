type UUID = string;
type ISODateString = string;

// ─────────────────────────────────────────────
// Core enums / unions
// ─────────────────────────────────────────────

type PlayerPosition =
  | "GK"
  | "CB"
  | "LB"
  | "RB"
  | "LWB"
  | "RWB"
  | "DM"
  | "CM"
  | "LM"
  | "RM"
  | "AM"
  | "LW"
  | "RW"
  | "ST"
  | "CF";

type PlayerStatus = "active" | "inactive" | "retired";

type PlayerOrigin =
  | "generated"
  | "academy"
  | "transferred"
  | "real";

type ContractStatus =
  | "active"
  | "expired"
  | "terminated"
  | "pending";

type ContractType =
  | "senior"
  | "youth"
  | "short_term";

type SquadRole =
  | "key_player"
  | "first_team"
  | "rotation"
  | "backup"
  | "prospect"
  | "youth"
  | null;

// ─────────────────────────────────────────────
// Generic attribute groups
// ─────────────────────────────────────────────

interface TechnicalAttributes {
  crossing: number;
  dribbling: number;
  finishing: number;
  first_touch: number;
  free_kicks: number;
  heading: number;
  long_shots: number;
  passing: number;
  penalties: number;
  tackling: number;
}

interface PhysicalAttributes {
  acceleration: number;
  agility: number;
  balance: number;
  jumping: number;
  natural_fitness: number;
  pace: number;
  stamina: number;
  strength: number;
}

interface MentalAttributes {
  anticipation: number;
  composure: number;
  concentration: number;
  decision_making: number;
  off_the_ball: number;
  positioning: number;
  teamwork: number;
  vision: number;
  work_rate: number;
}

interface TacticalAttributes {
  creativity: number;
  defensive_awareness: number;
  pressing: number;
  tempo_control: number;
}

interface PositionalAttributes {
  man_awareness: number;
  marking: number;
  positional_instinct: number;
  space_reading: number;
  versatility: number;
}

interface PlayerAttributes {
  technical: number;
  physical: number;
  mental: number;
  tactical: number;
  goalkeeping: number;
  positional: number;
}

interface AttributeValues {
  mental: MentalAttributes;
  physical: PhysicalAttributes;
  positional: PositionalAttributes;
  tactical: TacticalAttributes;
  technical: TechnicalAttributes;
}

// ─────────────────────────────────────────────
// Hidden / personality attributes
// ─────────────────────────────────────────────

interface HiddenAttributes {
  professionalism: number;
  temperament: number;
  adaptability: number;
  consistency: number;
  injury_susceptibility: number;
  ambition: number;
  loyalty: number;
  pressure_handling: number;
  learning_speed: number;
}

interface Personality {
  professionalism: number;
  ambition: number;
  loyalty: number;
  ego: number;
  sociability: number;
  adaptability: number;
  patience: number;
  leadership: number;
  emotional_volatility: number;
}

// ─────────────────────────────────────────────
// Career / condition
// ─────────────────────────────────────────────

interface PlayerCareer {
  appearances: number;
  goals: number;
  assists: number;
  average_rating: number;
}

interface PlayerCondition {
  fatigue: number;
  fitness: number;
  sharpness: number;
  injury_risk: number;
  tactical_familiarity: number;
  morale: number;
  playing_time_pct: number;
  updated_at: ISODateString;
}

// ─────────────────────────────────────────────
// Dossier
// ─────────────────────────────────────────────

interface PlayerBio {
  secondary_positions: PlayerPosition[];
  market_value: number;
  status: PlayerStatus;
  is_academy_product: boolean;
  origin: PlayerOrigin;
  country_id: UUID;
  developed_by_manager_id: UUID | null;
  created_at: ISODateString;
}

interface PlayerHistory {
  appearances: unknown[];
  attribute_changes: unknown[];
  injuries: unknown[];
  events: unknown[];
}

interface EmotionalState {
  emotional_state: string;
  cause: string;
  intensity: number;
  occurred_at: ISODateString;
  expires_at: ISODateString | null;
}

interface Contract {
  id: UUID;
  club_id: UUID;
  weekly_wage: number;
  signing_bonus: number;
  start_date: ISODateString;
  end_date: ISODateString;
  release_clause: number | null;
  playing_time_promise: string | null;
  status: ContractStatus;
  squad_role: SquadRole;
  contract_type: ContractType;
  created_at: ISODateString;
}

interface PlayerPrivate {
  transfer_request_cooldown_until: ISODateString | null;
  contracts: Contract[];
  emotional_states: EmotionalState[];
  preferences: unknown[];
  transfer_requests: unknown[];
}

interface PlayerDossier {
  bio: PlayerBio;
  attribute_values: AttributeValues;
  development: unknown | null;
  condition: PlayerCondition;
  personality: Personality;
  history: PlayerHistory;
  private: PlayerPrivate;
}

// ─────────────────────────────────────────────
// Player
// ─────────────────────────────────────────────

export interface Player {
  player: { id: UUID; name: string; };
  club: { id: UUID; name: string; };
  first_name: string;
  last_name: string;
  display_name: string;
  nationality: string;
  date_of_birth: string;
  position: PlayerPosition;
  squad_number: number;
  attributes: PlayerAttributes;
  overall: number;
  career: PlayerCareer;
  weekly_wage: number;
  hidden_attributes: HiddenAttributes;
  dossier: PlayerDossier;
}
