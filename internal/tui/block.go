package tui

import "time"

// ── Block kinds ────────────────────────────────────────────────────

type BlockKind string

const (
	BlockUser       BlockKind = "user"
	BlockAIThink    BlockKind = "ai_think"
	BlockAIToolUse  BlockKind = "ai_tool_use"
	BlockPCReturn   BlockKind = "pc_tool_return"
	BlockError      BlockKind = "error"
)

// ── Tool status ────────────────────────────────────────────────────

type ToolStatus string

const (
	ToolRunning ToolStatus = "running"
	ToolSuccess ToolStatus = "success"
	ToolFailed  ToolStatus = "failed"
)

// ── Block meta ─────────────────────────────────────────────────────

type BlockMeta struct {
	ToolName     string
	ToolInput    string
	ToolOutput   string
	ToolStatus   ToolStatus
	TokenInput   int
	TokenOutput  int
	RequestID    string
	CacheEnabled bool
}

// ── Block ──────────────────────────────────────────────────────────

type Block struct {
	Kind    BlockKind
	Content string
	Meta    BlockMeta

	// Nested blocks (tool_use → tool_return)
	Children []*Block

	// Pairing
	PairID  string
	Pending bool

	// Render cache
	Rendered    string
	RenderWidth int
	RenderDirty bool

	// Timestamps
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewBlock(kind BlockKind, content string) *Block {
	now := time.Now()
	return &Block{
		Kind:        kind,
		Content:     content,
		CreatedAt:   now,
		UpdatedAt:   now,
		RenderDirty: true,
	}
}

func (b *Block) AddChild(child *Block) {
	b.Children = append(b.Children, child)
}

func (b *Block) MarkDirty() {
	b.RenderDirty = true
	b.UpdatedAt = time.Now()
}

// ── Tool pairing ───────────────────────────────────────────────────

type toolPair struct {
	UseBlock  *Block
	ReturnBlock *Block
	StartTime time.Time
}

// ── Prefix config ──────────────────────────────────────────────────

type PrefixEntry struct {
	Text  string
	Color string
}

type PrefixConfig struct {
	User         PrefixEntry
	AIThink      PrefixEntry
	AIToolUse    PrefixEntry
	PCToolReturn PrefixEntry
}

// ── Old-kind migration ─────────────────────────────────────────────

// Deprecated: old blockKind values carried over during the refactor.
// Remove after all call sites are updated.
func migrateBlockKind(old string) BlockKind {
	switch old {
	case "user":
		return BlockUser
	case "ai":
		return BlockAIThink
	case "tools":
		return BlockAIToolUse
	case "error":
		return BlockError
	default:
		return BlockAIThink
	}
}
