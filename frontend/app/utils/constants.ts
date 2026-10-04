/**
 * Percentage coordinates (0-100) per slot, GK first then outfield.
 * y=92 sits just in front of goal; y decreases moving up the pitch
 * toward the attacking end — same convention as PitchView.vue.
 * Drop-in replacement for the FORMATION_COORDS map there.
 */
export const FORMATION_COORDS: Record<string, { x: number; y: number }[]> = {
  // Backend formations (squad.formationOrders): slot i must sit where the
  // backend's position i plays. Note the backend lists some midfield/attack
  // lines right-to-left (RW..LW, RM..LM), so x runs high-to-low there.
  '4-3-3': [ // GK LB CB CB RB | CM CM CM | RW ST LW
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 30, y: 52 }, { x: 50, y: 56 }, { x: 70, y: 52 },
    { x: 80, y: 26 }, { x: 50, y: 18 }, { x: 20, y: 26 },
  ],

  '4-4-2': [ // GK LB CB CB RB | RM CM CM LM | ST ST
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 85, y: 48 }, { x: 62, y: 54 }, { x: 38, y: 54 }, { x: 15, y: 48 },
    { x: 38, y: 20 }, { x: 62, y: 20 },
  ],

  '4-2-3-1': [ // GK LB CB CB RB | DM DM | RM AM LM | ST
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 38, y: 58 }, { x: 62, y: 58 },
    { x: 80, y: 38 }, { x: 50, y: 34 }, { x: 20, y: 38 },
    { x: 50, y: 16 },
  ],

  '5-3-2': [ // GK CB CB CB LB RB | CM CM CM | ST ST
    { x: 50, y: 92 },
    { x: 30, y: 76 }, { x: 50, y: 78 }, { x: 70, y: 76 }, { x: 10, y: 70 }, { x: 90, y: 70 },
    { x: 30, y: 50 }, { x: 50, y: 54 }, { x: 70, y: 50 },
    { x: 38, y: 20 }, { x: 62, y: 20 },
  ],

  '3-2-4-1': [ // GK CB CB CB | DM DM | RM AM AM LM | ST
    { x: 50, y: 92 },
    { x: 30, y: 76 }, { x: 50, y: 80 }, { x: 70, y: 76 },
    { x: 38, y: 60 }, { x: 62, y: 60 },
    { x: 88, y: 40 }, { x: 62, y: 36 }, { x: 38, y: 36 }, { x: 12, y: 40 },
    { x: 50, y: 16 },
  ],

  '5-4-1': [ // GK CB CB CB LB RB | LM CM CM RM | ST
    { x: 50, y: 92 },
    { x: 30, y: 76 }, { x: 50, y: 78 }, { x: 70, y: 76 }, { x: 10, y: 70 }, { x: 90, y: 70 },
    { x: 15, y: 46 }, { x: 38, y: 50 }, { x: 62, y: 50 }, { x: 85, y: 46 },
    { x: 50, y: 18 },
  ],

  '4-5-1': [ // GK LB CB CB RB | LM CM CM CM RM | ST
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 12, y: 46 }, { x: 32, y: 52 }, { x: 50, y: 56 }, { x: 68, y: 52 }, { x: 88, y: 46 },
    { x: 50, y: 18 },
  ],

  '3-5-2': [ // GK CB CB CB LB RB | CM CM CM | ST ST
    { x: 50, y: 92 },
    { x: 30, y: 76 }, { x: 50, y: 80 }, { x: 70, y: 76 }, { x: 10, y: 54 }, { x: 90, y: 54 },
    { x: 32, y: 50 }, { x: 50, y: 54 }, { x: 68, y: 50 },
    { x: 38, y: 20 }, { x: 62, y: 20 },
  ],

  // Not in the backend catalogue (ValidFormations) — unused until added there.
  '4-1-4-1': [
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 50, y: 62 },
    { x: 15, y: 44 }, { x: 38, y: 40 }, { x: 62, y: 40 }, { x: 85, y: 44 },
    { x: 50, y: 18 },
  ],

  '3-4-3': [
    { x: 50, y: 92 },
    { x: 30, y: 76 }, { x: 50, y: 80 }, { x: 70, y: 76 },
    { x: 12, y: 52 }, { x: 37, y: 50 }, { x: 63, y: 50 }, { x: 88, y: 52 },
    { x: 20, y: 22 }, { x: 50, y: 16 }, { x: 80, y: 22 },
  ],

  '4-4-1-1': [
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 15, y: 48 }, { x: 38, y: 54 }, { x: 62, y: 54 }, { x: 85, y: 48 },
    { x: 50, y: 32 },
    { x: 50, y: 16 },
  ],

  '4-1-2-1-2': [
    { x: 50, y: 92 },
    { x: 18, y: 74 }, { x: 38, y: 78 }, { x: 62, y: 78 }, { x: 82, y: 74 },
    { x: 50, y: 60 },
    { x: 30, y: 48 }, { x: 70, y: 48 },
    { x: 50, y: 34 },
    { x: 38, y: 18 }, { x: 62, y: 18 },
  ],
}

export const TACTICS = [
  {
    title: "Balanced",
    id: "balanced",
    about: "Balanced",
    formations: [`4-3-3`, `4-4-2`, `4-2-3-1`, `5-3-2`]
  },
  {
    title: "Possession Control",
    id: "possession",
    about: "Attack slightly technical/mental-led (passing, vision, composure); defense mental-led (interceptions / positioning)",
    formations: [`4-3-3`, `3-2-4-1`]
  },
  {
    title: "Gegenpress / High-Press",
    id: "gegenpress",
    about: "Attack *physical-led* (stamina, work rate, pressing → physical/ tactical). GK/defensive recipes unchanged",
    formations: [`4-3-3`, `4-2-3-1`]
  },
  {
    title: "Low-Block / Counter",
    id: "low_block",
    about: "Defense *tactical/technical-led* (positioning, tackling) and attack *physical- led * (pace)",
    formations: [`5-4-1`, `4-5-1`]
  },
  {
    title: "Direct / Long-Ball",
    id: "direct",
    about: "Attack *physical/technical-led* (strength, heading, jumping + finishing)",
    formations: [`4-4-2`, `3-5-2`]
  }
]