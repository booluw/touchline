package board

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"

	"github.com/google/uuid"
)

// Fan-reaction stories (IM33) are deterministic templates: the same fixture and
// club always produce the same story, so a replayed completion cannot differ.

var fanNames = []string{
	"Dave", "Sandra", "Mick", "Priya", "Tunde", "Carlos", "Aoife", "Yusuf",
	"Helen", "Gary", "Lucia", "Kwame", "Marta", "Stefan", "Joy", "Tommy",
}

var fanTags = []string{
	"a season-ticket holder", "who travels to every away game", "a supporter of thirty years",
	"from the supporters' trust", "who was in the stands today", "a lifelong fan",
}

// fanQuotes is keyed by mood band (see fanMood); %s is the manager's name.
var fanQuotes = [4][]string{
	{ // angry
		"I've seen enough. %s has lost this team and lost us.",
		"That was embarrassing. %s has to take the blame for it.",
		"We pay good money to watch that? %s is out of ideas.",
		"No plan, no fight. How long does %s get?",
	},
	{ // worried
		"I want %s to succeed, but I'm not sure where this is going.",
		"We keep saying next week. %s needs to find answers soon.",
		"There's something missing and %s doesn't seem to know what.",
		"I'm not calling for anyone's head, but %s has me nervous.",
	},
	{ // content
		"You can see what %s is trying to build. I'm happy enough.",
		"Steady progress. %s is doing a decent job with this group.",
		"Not perfect, but %s has us heading the right way.",
		"I trust %s. Give it time and it will come good.",
	},
	{ // delighted
		"Best I've felt about this club in years. %s is a genius.",
		"%s has the whole place buzzing. Long may it continue.",
		"That is exactly why we love %s. Brilliant.",
		"Give %s whatever contract is needed. We're going places.",
	},
}

// fanMood maps the post-match supporter sentiment and the match rating to a
// mood band: 0 angry, 1 worried, 2 content, 3 delighted.
func fanMood(sentiment, rating int) int {
	switch v := (sentiment + rating) / 2; {
	case v < 30:
		return 0
	case v < 50:
		return 1
	case v < 70:
		return 2
	default:
		return 3
	}
}

// fanSeed derives the stable story seed from the fixture and club.
func fanSeed(fixtureID, clubID uuid.UUID) int64 {
	h := fnv.New64a()
	h.Write(fixtureID[:])
	h.Write(clubID[:])
	return int64(h.Sum64())
}

// buildFanReaction writes the headline and body of one fan-reaction story.
func buildFanReaction(seed int64, clubName, managerName, opponent string, goalsFor, goalsAgainst, sentiment, rating int) (string, string) {
	if managerName == "" {
		managerName = "the manager"
	}
	r := rand.New(rand.NewSource(seed))
	mood := fanMood(sentiment, rating)

	result, verdict := "draw with", "split on"
	switch {
	case goalsFor > goalsAgainst:
		result = "win over"
	case goalsFor < goalsAgainst:
		result = "defeat to"
	}
	switch mood {
	case 0:
		verdict = "turn on"
	case 1:
		verdict = "uneasy about"
	case 2:
		verdict = "back"
	case 3:
		verdict = "hail"
	}
	headline := fmt.Sprintf("%s fans %s %s after %d-%d %s %s",
		clubName, verdict, managerName, goalsFor, goalsAgainst, result, opponent)

	var b strings.Builder
	fmt.Fprintf(&b, "Supporters gave their verdict outside the ground after %s's %d-%d %s %s.",
		clubName, goalsFor, goalsAgainst, result, opponent)
	names := r.Perm(len(fanNames))
	for i := 0; i < 2+r.Intn(2); i++ {
		// Most voices share the crowd's mood; one in four sits a band away.
		m := mood
		if r.Intn(4) == 0 {
			m = clamp(mood+r.Intn(3)-1, 0, 3)
		}
		quote := fmt.Sprintf(fanQuotes[m][r.Intn(len(fanQuotes[m]))], managerName)
		fmt.Fprintf(&b, "\n\n\"%s\" said %s, %s.", quote, fanNames[names[i]], fanTags[r.Intn(len(fanTags))])
	}
	return headline, b.String()
}
