package cli

// V0.usage-ledger (decision 0041): what a CLI says a turn used, in one
// shape, so the meter never reads a CLI's own JSON.

// TokenCount is the tokens of one model, by kind.
type TokenCount struct {
	Input        int64 `json:"input"`
	Output       int64 `json:"output"`
	CacheRead    int64 `json:"cacheRead"`
	CacheWrite5m int64 `json:"cacheWrite5m"`
	CacheWrite1h int64 `json:"cacheWrite1h"`
	Thinking     int64 `json:"thinking"`
	WebSearch    int64 `json:"webSearch"`
	WebFetch     int64 `json:"webFetch"`
}

// Add returns c + o.
func (c TokenCount) Add(o TokenCount) TokenCount {
	return TokenCount{c.Input + o.Input, c.Output + o.Output, c.CacheRead + o.CacheRead, c.CacheWrite5m + o.CacheWrite5m,
		c.CacheWrite1h + o.CacheWrite1h, c.Thinking + o.Thinking, c.WebSearch + o.WebSearch, c.WebFetch + o.WebFetch}
}

// Sub returns c − o.
func (c TokenCount) Sub(o TokenCount) TokenCount {
	return TokenCount{c.Input - o.Input, c.Output - o.Output, c.CacheRead - o.CacheRead, c.CacheWrite5m - o.CacheWrite5m,
		c.CacheWrite1h - o.CacheWrite1h, c.Thinking - o.Thinking, c.WebSearch - o.WebSearch, c.WebFetch - o.WebFetch}
}

// Min returns the smaller of c and o in each kind.
func (c TokenCount) Min(o TokenCount) TokenCount {
	m := func(a, b int64) int64 {
		if a < b {
			return a
		}
		return b
	}
	return TokenCount{m(c.Input, o.Input), m(c.Output, o.Output), m(c.CacheRead, o.CacheRead), m(c.CacheWrite5m, o.CacheWrite5m),
		m(c.CacheWrite1h, o.CacheWrite1h), m(c.Thinking, o.Thinking), m(c.WebSearch, o.WebSearch), m(c.WebFetch, o.WebFetch)}
}

// Less: some kind of c is below o's (a counter went down).
func (c TokenCount) Less(o TokenCount) bool {
	return c.Input < o.Input || c.Output < o.Output || c.CacheRead < o.CacheRead || c.CacheWrite5m < o.CacheWrite5m ||
		c.CacheWrite1h < o.CacheWrite1h || c.Thinking < o.Thinking || c.WebSearch < o.WebSearch || c.WebFetch < o.WebFetch
}

// Zero: no tokens and no requests.
func (c TokenCount) Zero() bool { return c == TokenCount{} }

// UsageScope says what a report's Models count.
type UsageScope string

const (
	// Cumulative: everything the session used so far, across processes
	// that resumed it (claude's result.modelUsage).
	Cumulative UsageScope = "cumulative"
	// PerTurn: what this turn used.
	PerTurn UsageScope = "turn"
)

// UsageReport is one result's usage.
type UsageReport struct {
	ID      string // the result's own id (claude: uuid), "" when it has none
	Session string
	Scope   UsageScope
	Models  map[string]TokenCount // model id → counts
	// Main: the main agent's share of this turn, when the CLI says it
	// (claude: result.usage); nil when it does not.
	Main *TokenCount
}

// UsageReader is an adapter that reads usage from its result messages.
type UsageReader interface {
	Usage(result Msg) (UsageReport, bool)
}

// Num reads a JSON number of a map as int64; 0 when absent.
func Num(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
