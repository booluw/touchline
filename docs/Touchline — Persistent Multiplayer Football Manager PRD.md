# Touchline
## Persistent Multiplayer Football Management Universe

### Product vision

Touchline is a browser-based multiplayer football management simulation in which thousands of managers participate in a persistent, evolving football world.

Players do not simply manage matches. They manage **people, clubs, money, reputations, relationships and careers**.

A manager should be able to begin as an unknown coach at a struggling club, build a dynasty, get fired, rebuild their reputation elsewhere, create a new club, become an owner, form a competition, or eventually become one of the most influential figures in the football world.

The central product promise is:

> **Every club has a personality. Every player has a story. Every decision has consequences.**

The game should produce stories that players want to tell other people.

Examples:

- “I got sacked despite finishing sixth because I destroyed the club financially.”
- “My academy graduate became the world's best striker and then left because I sold his best friend.”
- “We couldn't afford the star striker, so I built the best academy in the country instead.”
- “My biggest rival stole three of my players and we eventually met them in a continental final.”
- “I took a semi-professional club from the fourth tier to the Champions League.”
- “I shut down the academy to survive financially, then five years later regretted it.”
- “The board wanted promotion. I chose financial stability instead. They fired me.”
- “A player I developed 12 years ago came back as my assistant manager.”
- “I created a cup competition that eventually became one of the biggest competitions in the world.”

---

# 1. Product thesis

Traditional football-management games primarily optimize for **simulation depth**.

Touchline should optimize for:

1. **Social competition**
2. **Persistent world**
3. **Emergent stories**
4. **Club and player individuality**
5. **Meaningful decisions**
6. **Economic consequences**
7. **Long-term legacy**

Simulation depth remains extremely important, but it exists in service of the persistent multiplayer world.

The game should therefore avoid a common failure mode in management games: adding complexity simply because complexity is realistic.

Every system needs to answer:

> “Does this create a meaningful decision, story, rivalry or consequence?”

If not, automate it.

---

# 2. Target users

## Primary: Competitive football managers

Players who enjoy:

- Football Manager
- FIFA/EA FC career modes
- Online sports franchises
- OOTP
- Soccer Manager
- fantasy football
- strategy games
- management simulations

They want to beat other humans, not just an AI.

## Secondary: Football roleplayers

Players who care about:

- club identity
- academy development
- player relationships
- creating narratives
- becoming a legendary manager
- rebuilding fallen clubs

## Tertiary: Social football communities

Groups of friends, Discord communities, football communities and online leagues.

A major opportunity is to let a group create its own football universe.

---

# 3. Core product loop

The primary loop is:

**Observe → Decide → Act → Simulate → React → Adapt**

### Observe

The manager reviews:

- fixtures
- league position
- squad
- player morale
- injuries
- finances
- transfers
- board confidence
- training
- scouting
- news
- rivals
- competition developments

### Decide

The manager decides:

- who plays
- how the team trains
- who gets sold
- who gets bought
- contracts
- tactical approach
- academy investment
- staff
- finances
- long-term strategy

### Act

The manager:

- makes transfers
- schedules training
- negotiates contracts
- speaks to players
- changes tactics
- requests facilities
- interacts with board
- scouts players
- creates competitions

### Simulate

The world progresses.

Matches are played.

Players train.

Injuries occur.

Markets change.

AI clubs make decisions.

Players develop relationships.

Finances move.

### React

The manager receives consequences:

- player requests
- board decisions
- media stories
- transfer bids
- injuries
- dressing-room problems
- financial warnings
- rival activity

### Adapt

The manager changes strategy.

This creates the persistent loop.

---

# 4. Persistent multiplayer architecture

## Core principle

Touchline should **not require every manager to be online simultaneously**.

That is one of the biggest opportunities created by making this a browser-native game.

Conventional FM multiplayer can require participants to be online together and wait for everyone to progress. Community discussions explicitly identify this as a limitation.

Touchline should instead use an **asynchronous persistent world**.

## World clock

The game runs continuously.

Example:

- Monday 08:00 — training
- Monday 12:00 — player interactions
- Tuesday — league fixtures
- Wednesday — continental fixtures
- Thursday — training
- Friday — transfer activity
- Saturday/Sunday — matches

Managers can log in whenever they want.

## World ticks

The simulation operates through server-side ticks.

For example:

- 15-minute tactical simulation ticks during live matches
- hourly economic/social processing
- daily world processing
- weekly league processing
- monthly financial processing
- seasonal processing

The exact cadence should be configurable.

## Manager actions

Actions can be:

### Immediate

- change formation
- issue instructions
- shortlist player
- negotiate a contract

### Queued

- training programme
- scouting assignment
- facility request
- youth development plan

### Deadline-based

- match tactics
- competition registration
- transfer bids
- squad selection

This allows asynchronous play without reducing competitive integrity.

---

# 5. World structure

The world contains:

### Clubs

Each club has:

- identity
- finances
- reputation
- stadium
- facilities
- academy
- squad
- board
- supporters
- history
- rivals
- culture
- strategic philosophy

### Players

Each player has:

- ability
- potential
- personality
- emotional state
- relationships
- history
- nationality
- development trajectory
- ambitions
- contract
- injury state
- market value

### Managers

Each manager has:

- reputation
- tactical identity
- coaching ability
- personality
- history
- achievements
- relationships
- preferred clubs
- job status

### Competitions

Each competition has:

- rules
- clubs
- reputation
- prize money
- qualification
- scheduling
- history

### Football ecosystem

The world also contains:

- agents
- scouts
- coaches
- journalists
- owners
- sponsors
- supporters
- national associations
- governing bodies

---

# 6. Club DNA

This should be one of Touchline's most important systems.

A club is not merely:

> Reputation: 74  
> Budget: $50m

Instead, each club has a **Club DNA profile**.

## Club DNA dimensions

### Competitive ambition

0–100

Examples:

- survival
- stability
- mid-table
- continental qualification
- title challenge
- domestic dominance
- global dominance

### Financial philosophy

- conservative
- balanced
- aggressive
- debt-tolerant
- investor-funded
- self-sustaining

### Recruitment philosophy

- academy-first
- domestic youth
- international scouting
- undervalued players
- superstar recruitment
- free transfers
- veteran leadership

### Academy importance

0–100

### Patience

0–100

### Managerial control

0–100

### Star power preference

0–100

### Wage tolerance

0–100

### Selling philosophy

- never sell stars
- sell for large profit
- sell when replacement exists
- player-driven
- financially driven

### Tactical identity

Examples:

- possession
- counterattack
- pressing
- defensive
- direct
- adaptable

### Cultural identity

Examples:

- local
- international
- youth-oriented
- star-oriented
- working-class
- prestigious
- experimental

---

# 7. Club archetypes

Clubs can share archetypes but should never be identical.

Examples:

### The Giant

- expects trophies
- wants stars
- huge wages
- low patience
- high commercial expectations

### The Academy Club

- prioritizes youth
- accepts development seasons
- invests heavily in academy
- sells graduates strategically

### The Moneyball Club

- low wage spending
- analytics-heavy
- buys undervalued players
- sells at peak value

### The Community Club

- financially conservative
- strong local identity
- academy-focused
- tolerates mid-table finishes

### The Fallen Giant

- enormous reputation
- poor finances
- impatient supporters
- pressure to return to elite football

### The Investor Club

- aggressive spending
- high expectations
- willingness to accept losses

### The Survival Club

- survival is success
- low resources
- high financial sensitivity

---

# 8. Board system

The board is an active game system.

It should not simply say:

> “Board confidence: 72%.”

Instead, board confidence should emerge from several dimensions.

## Board variables

- results
- financial health
- transfer performance
- wage control
- academy performance
- player development
- supporter sentiment
- competition performance
- tactical alignment
- club philosophy
- promises
- manager reputation
- relationship with board
- ownership stability

## Board personalities

Examples:

### Patient Owner

Will tolerate poor results if the long-term plan is credible.

### Demanding Owner

Requires immediate success.

### Financial Conservative

May sack a manager despite good results if finances deteriorate.

### Academy Owner

Will tolerate poor league performance if youth development is excellent.

### Prestige Owner

Cares about stars and trophies.

### Political Board

Different board members have different agendas.

---

# 9. Manager sack system

Managers can be dismissed.

However, sackings must be explainable.

The system should calculate:

**Job Security = Performance + Expectations + Financials + Board Relationship + Club DNA + Supporter Sentiment + Alternatives**

## Sack triggers

Potential triggers include:

### Sporting

- repeated underperformance
- relegation
- failure to meet minimum target
- poor continental performance

### Financial

- unsustainable wage bill
- excessive transfer spending
- major losses
- debt deterioration
- failure to meet financial commitments

### Cultural

- abandoning academy strategy
- selling too many local players
- signing players contrary to club philosophy

### Relationship

- dressing-room collapse
- board conflict
- public criticism of ownership

### Behaviour

- repeated broken promises
- serious disciplinary issues

## Important design rule

Never make:

> “Finish 5th = fired”

the entire logic.

Instead:

> “Finish 5th at a club that expected 8th and improved finances = excellent.”

versus:

> “Finish 5th at a club expected to win the league, spend $300m and sign superstars = potentially disastrous.”

This directly addresses recurring community complaints that board expectations can become disconnected from resources and context.

---

# 10. The Board Contract

When joining a club, managers receive a **Board Mandate**.

Example:

### Arsenal

Primary:

- challenge for title

Secondary:

- Champions League quarter-final
- maintain wage structure
- sign high-reputation players

Strategic:

- produce two academy graduates

Financial:

- maintain positive operating balance

The manager can negotiate these targets.

This makes the employment contract meaningful.

---

# 11. Player simulation

Players should be treated as individuals.

Every player contains:

## Football attributes

- technical
- physical
- mental
- tactical
- goalkeeping
- positional

## Hidden variables

- potential
- consistency
- injury susceptibility
- adaptability
- professionalism
- ambition
- loyalty
- temperament
- pressure handling
- learning speed

## Personal variables

- preferred teammates
- disliked teammates
- favourite clubs
- former clubs
- preferred countries
- preferred managers
- family/location preferences
- language
- playing-time expectations
- wage expectations

---

# 12. Player personality

Instead of a single personality label, players have multiple dimensions.

### Professionalism

How seriously they approach football.

### Ambition

How aggressively they pursue success.

### Loyalty

How attached they are to club relationships.

### Ego

How strongly they expect status.

### Sociability

How easily they form relationships.

### Adaptability

How well they handle new environments.

### Patience

How long they tolerate being a substitute.

### Leadership

Ability to influence teammates.

### Emotional volatility

How strongly events affect them.

---

# 13. Player emotions

Emotions should be **stateful and contextual**.

A player doesn't simply have:

> Morale: 72

Instead:

### Emotional state

- happy
- content
- motivated
- frustrated
- anxious
- angry
- homesick
- excited
- betrayed
- ambitious
- confident
- isolated

### Emotional causes

Examples:

- dropped from starting XI
- new contract
- goal drought
- teammate sold
- promise fulfilled
- promise broken
- injury
- manager praise
- public criticism
- transfer bid rejected
- club winning
- club losing
- family issue

This makes emotions explainable.

---

# 14. Relationship graph

Every major character can have relationships with other entities.

A player may have:

**+82 friendship with Player A**

**+64 professional relationship with Manager**

**-41 rivalry with Player B**

**+91 loyalty to academy club**

**+72 relationship with former manager**

The relationship graph should evolve.

Relationships have types:

- friendship
- rivalry
- mentorship
- professional respect
- dislike
- family
- national-team relationship
- academy relationship
- former teammate
- former manager
- agent relationship

---

# 15. Squad dynamics

The dressing room should emerge from the relationship graph.

Possible groups:

### Leadership group

Senior players with influence.

### Young core

Young players who socialize together.

### Foreign group

Players connected through nationality/language.

### Academy group

Graduates with shared history.

### New arrivals

Players who haven't integrated.

### Tactical group

Players who believe in the manager's system.

The game should calculate:

- squad cohesion
- trust
- leadership
- faction strength
- dressing-room mood
- manager support

---

# 16. Player interactions

A major product principle:

> **No arbitrary dialogue outcomes.**

Community criticism repeatedly identifies player interactions as tedious and sometimes irrational.

Touchline should model why a player reacts.

For example:

Manager:

> “You're doing excellent work in training.”

Player reaction depends on:

- recent training performance
- personality
- confidence
- manager relationship
- expectations
- whether praise is public/private
- previous praise frequency

The player might say:

> “Thanks, boss. I'm finally feeling settled.”

Or:

> “I appreciate that, but I still think I need more minutes.”

Both are plausible.

---

# 17. Player promises

Promises become contractual commitments.

Examples:

- increase playing time
- sign better players
- improve facilities
- allow transfer
- play player in preferred position
- loan player
- promote academy player
- improve salary
- challenge for trophy

Every promise has:

- explicitness
- deadline
- confidence
- importance
- consequence

Managers can negotiate promises.

Broken promises damage trust.

But the game should understand context.

If a manager promised a player “regular football” and then the player becomes injured, the promise should adapt.

---

# 18. Transfer requests

Players can request transfers.

Triggers include:

- lack of playing time
- broken promises
- wage dissatisfaction
- club ambition
- relationship problems
- desire to return home
- bigger club interest
- manager conflict
- squad competition

Transfer requests should not automatically mean:

> “Sell me immediately.”

The player may instead say:

> “I want you to listen to offers.”

That distinction creates richer negotiation.

---

# 19. Transfer market

The transfer market is a core multiplayer battlefield.

Every player has:

- valuation
- asking price
- wage expectation
- transfer desire
- market demand
- agent expectation
- club importance
- contract situation

## Market demand

Demand should be dynamic.

A player who scores 20 goals can suddenly attract:

- human managers
- AI clubs
- scouts
- media attention
- sponsors

Community players specifically complain when strong performances fail to generate realistic market interest.

Touchline should therefore create **event-driven market demand**.

---

# 20. Transfer negotiations

Negotiation should include:

### Transfer fee

### Installments

### Bonuses

- appearances
- goals
- trophies
- international appearances
- promotion

### Sell-on clauses

### Buy-back clauses

### Release clauses

### Player exchange

### Loan-to-buy

### Salary

### Signing bonus

### Agent fee

### Playing-time promises

### Contract length

Managers can negotiate with clubs and players independently.

---

# 21. Human-to-human transfers

A transfer between two managers should feel like a negotiation.

Example:

> Manchester City bids $72m.

You can:

- accept
- reject
- counter
- ask for player exchange
- request sell-on percentage
- ask them to wait
- leak the bid
- tell player you're willing to sell
- promise player a move later

This becomes social gameplay.

---

# 22. Agents

Agents should be semi-autonomous actors.

Each agent has:

- negotiation ability
- reputation
- aggressiveness
- loyalty
- preferred clubs
- player relationships
- fee expectations

Agents can influence:

- contract negotiations
- transfer requests
- rumours
- player happiness

This also creates an eventual path toward an **agent game mode**.

---

# 23. Academy system

Most clubs should have an academy where financially and structurally appropriate.

Academy has:

- facilities
- youth recruitment
- coaching
- scouting network
- staff quality
- regional reach
- academy reputation

## Academy investment

Managers can allocate:

- capital
- wages
- recruitment budget
- coaching budget
- facilities

## Academy output

Every season produces prospects based on:

- region
- club reputation
- academy quality
- coaching
- recruitment
- randomness
- football culture

---

# 24. Academy shutdown

This should be a genuine strategic decision.

A financially struggling club might decide:

> Close academy.

Immediate benefits:

- lower wages
- lower facilities cost
- lower recruitment cost

Long-term costs:

- fewer homegrown players
- reduced club identity
- lower future transfer income
- supporter backlash
- weaker long-term squad pipeline

Reopening should be expensive and slow.

This creates real strategic trade-offs.

---

# 25. Player development

Player development is not simply:

> Training +1 attribute.

Development depends on:

- age
- potential
- training
- minutes
- coaching
- professionalism
- injuries
- tactical fit
- confidence
- environment
- competition level
- relationships
- academy quality

Potential should be **dynamic rather than entirely fixed**.

A young player who consistently performs beyond expectations can expand their ceiling.

A player who stagnates can fail to reach their theoretical potential.

---

# 26. Training

Managers can control:

### Team training

- fitness
- tactics
- possession
- attacking
- defending
- set pieces
- pressing

### Individual training

- technical attributes
- position
- role
- weaknesses

### Development programmes

- wonderkid
- first-team integration
- physical development
- tactical education

### Load

- intensity
- recovery
- rest

Training must affect:

- development
- fatigue
- injury risk
- tactical familiarity
- morale

---

# 27. Injuries

Injuries are part of the simulation.

Types:

- muscle
- ligament
- bone
- concussion
- illness
- recurring injury

Each has:

- severity
- expected recovery
- uncertainty
- recurrence risk

Medical staff quality matters.

Players can also experience:

- rushed recovery
- setbacks
- fitness concerns
- psychological effects

---

# 28. Player retirement

Players age through careers.

Retirement depends on:

- age
- ability
- injuries
- finances
- ambition
- reputation
- personality

Retiring players can become:

- coaches
- scouts
- agents
- executives
- pundits
- academy staff

This creates continuity.

---

# 29. Club history

Everything important should be recorded.

For each club:

- trophies
- promotions
- relegations
- legendary players
- legendary managers
- record transfers
- biggest wins
- biggest defeats
- academy graduates
- rivalries
- financial crises
- stadium history

Players should be able to explore club history.

---

# 30. Football pyramid

Touchline should support multiple tiers.

Example:

### Tier 1
Global elite

### Tier 2
Major professional

### Tier 3
National professional

### Tier 4
Semi-professional

### Tier 5+
Regional/amateur

Each tier has different:

- revenue
- wages
- player quality
- attendance
- scouting
- sponsorship
- facilities
- competition rules
- transfer behaviour

This is important because managing a sixth-tier club should not feel like managing a Champions League club with smaller numbers.

---

# 31. Promotion and relegation

Promotion should radically change club economics.

Promotion brings:

- higher revenue
- higher expectations
- better player interest
- stronger sponsors
- higher wages
- stronger competition

Relegation causes:

- revenue collapse
- player departures
- wage restructuring
- supporter pressure
- debt problems

Clubs should sometimes become financially trapped after relegation.

---

# 32. Competitions

The world supports:

- domestic leagues
- domestic cups
- continental competitions
- regional competitions
- youth competitions
- preseason competitions
- international competitions
- custom competitions

---

# 33. Manager-created competitions

Managers can create competitions.

Examples:

> “African Rising Stars Cup”

> “European Owners Cup”

> “Discord Champions League”

Competition creator chooses:

- name
- participating clubs
- qualification
- format
- prize money
- scheduling
- home/away
- entry requirements
- registration rules

The system must prevent abuse.

For example:

- minimum reputation
- maximum prize budget
- association approval
- scheduling conflicts
- entry fees

---

# 34. Competition reputation

Competitions develop reputations.

A new cup starts at:

> Reputation: 10

If it attracts:

- major clubs
- spectators
- sponsors
- high-quality matches

its reputation increases.

Eventually it can become prestigious.

This means managers can literally **create football institutions**.

---

# 35. Finance engine

Financials should be one of Touchline's deepest systems.

Every club has:

### Revenue

- ticket sales
- season tickets
- sponsorship
- broadcasting
- prize money
- merchandise
- player sales
- loans
- commercial partnerships

### Costs

- wages
- transfers
- staff
- facilities
- academy
- stadium
- travel
- medical
- scouting
- debt interest
- bonuses

---

# 36. Cash vs budget

These must be different concepts.

A club can have:

> $20m transfer budget

but:

> $3m available cash.

This is realistic because budgets represent planned future income and spending capacity, not simply cash sitting in a bank account.

The financial model should explicitly expose:

- cash
- committed spending
- future transfer installments
- projected revenue
- wage commitments
- debt
- operating profit
- projected year-end balance

This addresses a recurring player discussion around the confusing distinction between club balance and transfer budget.

---

# 37. Financial crisis

Clubs can enter crisis.

Stages:

### Warning

Cash-flow problems.

### Restriction

Board reduces spending.

### Emergency

Players must be sold.

### Administration risk

Major restructuring.

### Ownership intervention

Investor/cash injection.

### Bankruptcy

Extremely rare and heavily protected by league rules.

The objective isn't to randomly destroy clubs.

It is to make financial management meaningful.

---

# 38. Ownership

Clubs can experience:

- ownership changes
- takeovers
- investment
- debt restructuring
- board changes

A new owner can completely change club DNA.

Example:

Old owner:

> Academy-first.

New owner:

> Buy superstars.

The manager must adapt or leave.

---

# 39. Supporters

Fans have their own expectations.

Fan variables:

- patience
- ambition
- loyalty
- identity
- rivalry intensity
- financial sensitivity

Supporters care about different things.

One club's supporters may celebrate:

> 8th place + academy graduates.

Another may consider:

> 8th place a disaster.

---

# 40. Media and news

The football world needs a narrative layer.

Events create stories:

- transfer rumours
- manager sackings
- player disputes
- wonderkids
- financial problems
- tactical innovations
- rivalries
- controversies

News should be generated from actual world events.

It should not be a meaningless stream of generic articles.

---

# 41. Rivalries

Rivalries can emerge.

Club rivalry strength depends on:

- geographic proximity
- competition
- history
- controversial transfers
- title races
- repeated matches
- fan sentiment
- manager relationships

Human managers can develop personal rivalries too.

---

# 42. Manager reputation

Manager reputation evolves through:

- trophies
- promotions
- player development
- finances
- tactical innovation
- academy success
- media presence

A manager can become known for:

- youth development
- financial discipline
- attacking football
- winning trophies
- developing strikers
- signing undervalued players

This reputation affects job offers.

---

# 43. Manager career

Managers can:

- join clubs
- resign
- get fired
- take sabbaticals
- retire
- create clubs
- become owners

## Starting a club

Advanced managers can establish a new club.

They select:

- location
- name
- colours
- identity
- philosophy
- starting financial model
- stadium
- academy strategy

But new clubs should have meaningful constraints.

A manager shouldn't be able to create:

> “Manchester Super FC”

with $1 billion.

Club creation should involve:

- financing
- league placement
- facilities
- reputation
- board structure
- supporter creation

---

# 44. Manager ownership path

Eventually managers can accumulate enough reputation/capital to:

- buy a club
- become chairman
- establish a club
- invest in football infrastructure

This creates an endgame beyond simply winning trophies.

---

# 45. AI clubs

AI clubs are not just opponents.

Each AI club should have:

- goals
- philosophy
- budget
- personality
- decision model

The AI should decide:

> “What would this club do?”

rather than:

> “What is the optimal move?”

This is essential for individuality.

---

# 46. AI transfer behaviour

AI clubs evaluate players using:

- tactical fit
- age
- finances
- reputation
- club philosophy
- squad need
- manager preferences
- board expectations

A youth-focused club shouldn't suddenly buy a 34-year-old superstar because his rating is high.

---

# 47. AI manager behaviour

Managers should have:

- tactical philosophy
- risk tolerance
- transfer preferences
- favourite formations
- player preferences
- personality
- ambition

Human managers should learn the tendencies of rival managers.

---

# 48. Multiplayer social layer

The multiplayer layer is the product's differentiator.

Managers can:

- message one another
- negotiate transfers
- form alliances
- create competitions
- develop rivalries
- share scouting information
- loan players
- arrange friendlies
- form ownership groups
- create leagues

Optional social systems:

- clubs can have Discord-like communities
- league commissioners
- league chat
- transfer announcements
- press conferences

---

# 49. Social trust

Managers develop reputations with other managers.

A manager who:

- honours agreements
- negotiates fairly
- pays on time
- doesn't exploit loopholes

gets a reputation for being trustworthy.

A manager who:

- breaks agreements
- manipulates rules
- deliberately trolls competitions

loses social reputation.

This should be separate from football reputation.

---

# 50. Anti-abuse system

Persistent multiplayer introduces serious risks.

We need:

- trade monitoring
- suspicious transfer detection
- collusion detection
- bot detection
- multi-account detection
- market manipulation monitoring
- commissioner controls
- dispute resolution
- transaction history

The game should never punish legitimate unconventional strategies merely because they are unusual.

---

# 51. Competitive integrity

No pay-to-win.

Players should never be able to buy:

- better players
- guaranteed victories
- unlimited money
- attribute boosts
- transfer advantages

Potential monetization:

- cosmetic club customization
- premium analytics
- historical/statistical tools
- optional league hosting
- expanded cosmetic features

Competitive football outcomes remain earned.

---

# 52. User interface

The UI should feel like a modern football operating system.

Primary navigation:

**Home**

**Squad**

**Tactics**

**Training**

**Transfers**

**Scouting**

**Academy**

**Finances**

**Club**

**Competitions**

**World**

**Career**

**Social**

---

# 53. Home dashboard

The dashboard should answer:

> “What needs my attention?”

It should show:

### Urgent

- injured player
- transfer deadline
- board warning
- contract expiry
- financial crisis

### Important

- player request
- scouting report
- training issue
- upcoming opponent

### Interesting

- rival transfer
- wonderkid emergence
- competition announcement
- media story

The manager should never need to inspect 20 screens just to discover that a player is furious.

---

# 54. Explainability

Every major system must answer:

> Why?

Examples:

**Why is the board unhappy?**

> “Your wage bill is 18% above the club's approved structure and you have failed to qualify for Europe.”

**Why does the player want to leave?**

> “Playing time: -18  
> Broken promise: -12  
> Club ambition: -8  
> Relationship with manager: -6”

**Why did this club bid for my player?**

> “Their starting striker is injured for 4 months and your player matches their tactical profile.”

This is one of Touchline's most important UX principles.

---

# 55. Matchday

The match experience should balance simulation depth with browser accessibility.

Managers can choose:

### Full tactical control

Live adjustments.

### Assisted

Assistant recommends changes.

### Pre-match only

Set tactics and let the match simulate.

### Quick result

For matches where the user doesn't want to watch.

The simulation continues regardless.

---

# 56. Tactical engine

Tactics include:

- formation
- roles
- instructions
- mentality
- pressing
- defensive line
- possession
- tempo
- width
- transitions
- set pieces

But the match engine must also account for:

- player personality
- fatigue
- confidence
- relationships
- tactical familiarity
- opposition adaptation

---

# 57. Match consequences

Matches affect:

- confidence
- morale
- reputation
- injuries
- board confidence
- fan sentiment
- player development
- manager reputation
- finances
- rivalries

A derby shouldn't feel identical to a meaningless mid-season match.

---

# 58. Seasonal structure

A season contains:

### Preseason

- transfers
- training camp
- friendlies
- squad building

### Opening

- tactics
- registration
- expectations

### Midseason

- transfer window
- injuries
- board review
- youth development

### Run-in

- title races
- relegation battles
- continental qualification

### End

- awards
- contracts
- transfers
- academy intake
- board review

### Offseason

- transfers
- staff
- facilities
- financial planning
- competition scheduling

---

# 59. Awards

Awards should generate prestige.

Examples:

- Player of the Year
- Young Player
- Manager of the Year
- Golden Boot
- Best Academy
- Best Transfer
- Best Goalkeeper
- Best XI

Awards become part of player history.

---

# 60. Records

Track:

- most goals
- most assists
- appearances
- trophies
- biggest transfers
- longest-serving managers
- largest wins
- best unbeaten runs
- richest clubs
- biggest academy sales

This drives long-term legacy.

---

# 61. The world should remember

If a manager leaves a club, their history remains.

Future managers should see:

> “Under James Adeyemi, the club won three league titles and produced six academy graduates.”

Players should remember:

> “Developed by manager X.”

Clubs remember:

> “Sold to Club Y for record fee.”

This creates emotional attachment.

---

# 62. Technical architecture

## Frontend

Recommended:

- React
- TypeScript
- Next.js
- responsive browser UI

The game must be designed desktop-first but usable on tablet/mobile.

## Backend

Recommended:

- TypeScript/Node.js or Go
- PostgreSQL
- Redis
- event-driven job processing

## Simulation engine

The simulation should be a separate service/module.

It should process:

- matches
- player development
- finances
- transfers
- relationships
- injuries
- competitions
- world events

---

# 63. Event-driven architecture

Every important world event becomes an event.

Examples:

```text
PLAYER_SCORED_GOAL
PLAYER_INJURED
PLAYER_REQUESTED_TRANSFER
CONTRACT_EXPIRED
TRANSFER_BID_RECEIVED
PLAYER_SIGNED
MANAGER_SACKED
BOARD_CHANGED
CLUB_PROMOTED
CLUB_RELEGATED
ACADEMY_PROSPECT_CREATED
COMPETITION_WON
FINANCIAL_WARNING
```

Events can trigger secondary effects.

Example:

```text
PLAYER_SOLD
    ↓
Friendship relationship disrupted
    ↓
Teammate morale decreases
    ↓
Player requests meeting
    ↓
Media reports dressing-room concern
    ↓
Board monitors squad atmosphere
```

This is how we create emergent stories.

---

# 64. Simulation state

The authoritative game state belongs on the server.

Client:

> Displays state and submits commands.

Server:

> Validates command.

Simulation:

> Applies event.

Database:

> Persists new state.

Example:

```text
Manager submits:
SELL PLAYER

↓

Server validates:
- Is player registered?
- Is transfer window open?
- Is bid valid?
- Is manager authorised?

↓

Transfer engine executes

↓

Events generated

↓

Relationship engine reacts

↓

Finance engine updates

↓

Squad dynamics update

↓

News engine generates story

↓

All affected managers receive notifications
```

---

# 65. Data model

Core entities:

```text
User
Manager
Club
Player
Staff
Agent
Owner
Board
SupporterGroup
League
Competition
Fixture
Match
Transfer
Contract
TrainingPlan
Injury
Relationship
FinanceAccount
Facility
Academy
NewsStory
Message
ManagerAgreement
ClubHistory
PlayerHistory
WorldEvent
```

---

# 66. Relationship data model

Relationships should be graph-like.

```text
Relationship
    entityA
    entityB
    type
    strength
    trust
    history
    lastInteraction
    sentiment
```

This enables complex social simulation.

---

# 67. Financial ledger

Do not store only:

```text
balance = $4m
```

Store transactions.

Example:

```text
TV Revenue       +$2m
Ticket Revenue   +$400k
Wages            -$800k
Staff             -$100k
Transfer Fee     -$1.5m
Loan Repayment   -$250k
```

The financial dashboard can therefore explain exactly where money went.

---

# 68. Simulation integrity

The simulation engine must be deterministic where possible.

Given:

```text
World State
+
Inputs
+
Random Seed
```

the engine should produce:

```text
Same Result
```

This is important for:

- debugging
- anti-cheat
- replays
- disputes
- competition integrity

---

# 69. Scaling

The architecture should support thousands of clubs.

World processing should be distributed.

Potential architecture:

```text
Web Client
    |
API Gateway
    |
Application Services
    |
--------------------------------
|      |       |       |       |
Match  Finance Transfer Social World
Engine Engine  Engine  Engine Engine
    |
Event Bus
    |
Simulation Workers
    |
PostgreSQL + Redis
```

---

# 70. Save architecture

Unlike a traditional local save:

> The world itself is the save.

Users log into the same persistent universe.

If they don't play for a week:

- their club continues operating
- assistants execute configured policies
- contracts continue
- matches occur
- finances evolve

This makes the world feel alive.

---

# 71. Absence mode

Managers need protection from being offline.

Set policies:

### Transfers

> Never sell first-team players below $X.

### Training

> Assistant handles training.

### Squad

> Assistant selects team.

### Finances

> Do not exceed wage budget.

### Contract

> Automatically renew academy players under threshold.

### Matches

> Use default tactics.

This makes persistent multiplayer viable.

---

# 72. Manager delegation

Managers can delegate:

- training
- scouting
- transfers
- contracts
- youth development
- media
- match preparation

Delegation is important because deep simulation can otherwise become a chore.

---

# 73. Complexity principle

Every system gets three modes:

### Simple

The assistant handles it.

### Standard

The manager makes meaningful decisions.

### Advanced

The manager controls everything.

This allows both casual and hardcore players to inhabit the same world.

---

# 74. MVP

The MVP should **not** attempt the entire football universe.

The first playable version should contain:

### World

- persistent leagues
- clubs
- players
- managers

### Football

- fixtures
- matches
- tactics
- league tables
- promotion/relegation

### Management

- squad
- training
- transfers
- contracts

### Social

- manager profiles
- messaging
- human transfers
- rivalries

### Economics

- wages
- transfer budgets
- revenue
- expenses

### Board

- expectations
- confidence
- sackings

### Player dynamics

- morale
- relationships
- playing time
- transfer requests

This is enough to validate the core product.

---

# 75. V1

Add:

- academies
- advanced player personalities
- detailed finances
- agents
- scouting
- staff
- injuries
- dynamic player development
- competitions
- media
- club identities
- supporters
- ownership changes

---

# 76. V2

Add:

- club creation
- manager ownership
- custom competitions
- national football
- deeper international markets
- advanced relationship graphs
- retired player careers
- staff careers
- advanced sponsorship
- stadium development
- global football economy

---

# 77. V3: Football Universe

The long-term vision is not simply a manager game.

It is a **football MMO/simulation ecosystem**.

Players could eventually inhabit different roles:

- manager
- sporting director
- owner
- agent
- journalist
- scout
- player

The same football world persists underneath them.

---

# 78. Monetization

Recommended:

## Free core game

Players can manage clubs without paying for competitive advantages.

## Premium features

Possible:

- advanced analytics
- historical databases
- enhanced customization
- advanced scouting tools
- cosmetic club customization
- private league hosting
- commissioner tools

## Never sell

- player attributes
- wins
- money
- guaranteed transfers
- training boosts
- premium players

The economic simulation is the competitive foundation and must remain trustworthy.

---

# 79. Key product metrics

### North Star Metric

**Meaningful Manager Decisions per Active Manager per Week**

Not simply sessions.

A meaningful decision includes:

- transfer
- contract
- tactical decision
- training decision
- player interaction
- financial decision
- board decision
- competition decision

## Supporting metrics

### Retention

- D1
- D7
- D30
- D90

### World health

- active clubs
- active managers
- human-vs-human fixtures
- transfers
- negotiations
- competitions created

### Social

- manager-to-manager interactions
- trades
- messages
- rivalries
- shared competitions

### Depth

- average club tenure
- seasons completed
- player careers observed
- academy graduates
- transfers per season

---

# 80. What success looks like

A successful Touchline save should eventually generate statements like:

> “I hate Chelsea because their manager stole my academy striker.”

> “My board doesn't care about trophies; they just want financial stability.”

> “This 17-year-old is going to be the next superstar.”

> “I can't sell him. He's the captain and half the dressing room will revolt.”

> “We had to close our academy to survive.”

> “The club is now worth 20 times what it was when I joined.”

> “I was fired, joined our biggest rival, and won the league against my old club.”

These are not scripted narratives.

They emerge from the simulation.

---

# 81. Core design principles

### Principle 1 — Every club is different

Club DNA drives behaviour.

### Principle 2 — Every player is different

Players have personality, history and relationships.

### Principle 3 — Explain consequences

Never make the player wonder why something happened.

### Principle 4 — Money matters

Financial decisions should create genuine trade-offs.

### Principle 5 — Humans create the drama

The multiplayer world is the primary source of stories.

### Principle 6 — The world remembers

Actions should have historical consequences.

### Principle 7 — Don't simulate chores

Automate repetitive management.

### Principle 8 — Don't punish experimentation

Unexpected strategies should be viable.

### Principle 9 — Avoid arbitrary emotions

Player reactions need understandable causes.

### Principle 10 — Avoid fake complexity

More numbers do not automatically mean a better simulation.

---

# 82. The most important differentiator

Touchline's ultimate competitive advantage should be its **World Relationship Engine**.

Football is fundamentally relational.

Players have:

- teammates
- managers
- agents
- former clubs
- rivals
- friends
- mentors
- national teammates

Clubs have:

- rivals
- partners
- transfer relationships
- historical enemies
- academy relationships

Managers have:

- former clubs
- former players
- rival managers
- trusted agents
- board relationships

These relationships interact with:

- transfers
- performances
- contracts
- finances
- media
- results

That produces a world that feels alive.

---

# 83. Example emergent story

A club has a promising 19-year-old striker.

His best friend is the team's 20-year-old winger.

The striker becomes first-choice.

The manager receives a $40m bid.

The club is financially unstable.

The board recommends selling.

The manager sells.

The winger becomes unhappy.

The manager tries to reassure him.

The winger asks for a new contract.

The manager refuses because of finances.

The winger requests a transfer.

Another human manager bids.

The winger joins them.

Six months later, both players are starting for the rival.

The clubs meet in a cup semifinal.

The former winger scores.

He celebrates in front of his old supporters.

The manager's job security falls because the board believes the squad lost too much attacking quality.

The academy produces another striker.

The manager promotes him.

Three seasons later, that player becomes a club legend.

**That is the game.**

Not the menu screens.

Not the attribute numbers.

The story.

---

# 84. Product positioning

The positioning should be:

> **The persistent multiplayer football universe.**

Not:

> “A browser football manager.”

The former creates an entirely different category.

Football Manager asks:

> “Can you manage a football club?”

Touchline asks:

> **“What kind of football world will you create?”**

---

# 85. Final product strategy

The first version should therefore be deliberately narrower than the ultimate vision.

The development order should be:

**1. Multiplayer world**

→ **2. Clubs**

→ **3. Players**

→ **4. Matches**

→ **5. Transfers**

→ **6. Finances**

→ **7. Relationships**

→ **8. Board/ownership**

→ **9. Academies**

→ **10. Competitions**

→ **11. World/legacy**

The mistake would be attempting to build every Football Manager feature before validating the central multiplayer loop.

The first question to prove is:

> **“Is managing a club in a world populated by other real managers more compelling than managing a club alone?”**

If the answer is yes, every subsequent system has a reason to exist.

The long-term ambition is much larger:

> **Touchline should become a persistent football society rather than simply a football management game.**