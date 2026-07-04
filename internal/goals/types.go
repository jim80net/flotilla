package goals

// File is the compiled fleet-goals.json on-disk shape (canonical contract for internal/dash).
type File struct {
	Version     int    `json:"version,omitempty"`
	DefaultView bool   `json:"default_view,omitempty"`
	Goals       []Goal `json:"goals"`
}

// Goal is one node in the flat goals list (parent refs, not nested children).
type Goal struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Description       string     `json:"description,omitempty"`
	Scope             string     `json:"scope,omitempty"`
	Parent            string     `json:"parent,omitempty"`
	Owner             string     `json:"owner,omitempty"`
	Status            string     `json:"status,omitempty"`
	ConversationAgent string     `json:"conversation_agent,omitempty"`
	TopologyChannelID string     `json:"topology_channel_id,omitempty"`
	Priorities        []string   `json:"priorities,omitempty"`
	Milestones        []string   `json:"milestones,omitempty"`
	DependsOn         []string   `json:"depends_on,omitempty"`
	WorkItems         []WorkItem `json:"work_items,omitempty"`
	Brief             string     `json:"brief,omitempty"` // goal-level decision package (#347/#349)
}

// WorkItem is one unit of work attached to a goal node (canonical #277 field names).
type WorkItem struct {
	Kind  string `json:"kind"`
	Agent string `json:"agent,omitempty"` // kind=desk
	Match string `json:"match,omitempty"` // kind=backlog: marker text or substring key
	Ref   string `json:"ref,omitempty"`   // kind=issue
	Text  string `json:"text,omitempty"`  // kind=inline
	Done  bool   `json:"done,omitempty"`  // kind=inline
	Label string `json:"label,omitempty"`
	Brief string `json:"brief,omitempty"` // work-item decision package (#347/#349)
}
