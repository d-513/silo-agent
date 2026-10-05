package tunnels

import (
	"fmt"
	"math/rand/v2"
)

// A tunnel's name is its subdomain: three plain lowercase words, adjective
// colour animal, like quiet-amber-heron. Short enough to read out, large enough
// (~290k) that a collision is a retry, never a limit.
var (
	adjectives = []string{
		"agile", "ancient", "bold", "brave", "breezy", "bright", "brisk", "calm",
		"clever", "cosmic", "crisp", "curious", "daring", "dapper", "eager", "easy",
		"fancy", "fierce", "fluffy", "gentle", "giddy", "golden", "graceful", "happy",
		"hardy", "humble", "jolly", "keen", "kind", "lively", "lucky", "mellow",
		"merry", "mighty", "nimble", "noble", "odd", "patient", "peppy", "plucky",
		"polite", "proud", "quick", "quiet", "rapid", "royal", "rustic", "shy",
		"silent", "sleek", "snappy", "soft", "spry", "steady", "sunny", "swift",
		"tidy", "tiny", "vivid", "warm", "wild", "wise", "witty", "zesty",
	}
	colors = []string{
		"amber", "aqua", "azure", "beige", "black", "blue", "bronze", "brown",
		"cobalt", "copper", "coral", "cream", "crimson", "cyan", "denim", "ebony",
		"emerald", "fuchsia", "gold", "gray", "green", "indigo", "ivory", "jade",
		"khaki", "lavender", "lemon", "lilac", "lime", "magenta", "maroon", "mint",
		"navy", "olive", "orange", "orchid", "peach", "pearl", "pink", "plum",
		"purple", "red", "rose", "ruby", "rust", "sage", "salmon", "sand",
		"scarlet", "silver", "slate", "tan", "teal", "violet", "white", "yellow",
	}
	animals = []string{
		"badger", "bat", "bear", "beaver", "bison", "camel", "cat", "cheetah",
		"cobra", "condor", "coyote", "crane", "crow", "deer", "dingo", "dolphin",
		"dove", "eagle", "falcon", "ferret", "finch", "fox", "gecko", "goat",
		"gull", "hare", "hawk", "heron", "horse", "ibis", "jackal", "jaguar",
		"koala", "lemur", "lion", "lynx", "marten", "mole", "moose", "newt",
		"otter", "owl", "panda", "parrot", "pika", "puma", "quail", "raven",
		"robin", "seal", "shark", "sloth", "stoat", "swan", "tiger", "toad",
		"trout", "turtle", "viper", "walrus", "weasel", "whale", "wolf", "wren",
	}
)

// NewName returns a fresh adjective-colour-animal name.
func NewName() string {
	return fmt.Sprintf("%s-%s-%s",
		adjectives[rand.IntN(len(adjectives))],
		colors[rand.IntN(len(colors))],
		animals[rand.IntN(len(animals))])
}
