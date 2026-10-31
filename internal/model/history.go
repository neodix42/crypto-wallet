package model

// HistoryTransfer is one explicitly interpreted public ledger effect. Atomic
// quantities retain exact contract precision. Empty peers/comments are unknown
// or absent, not guessed from an approval or a provider's descriptive text.
type HistoryTransfer struct {
	ID          string `json:"id"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Asset       Asset  `json:"asset"`
	Atomic      string `json:"atomic"`
	// Comment is a decoded on-chain memo/comment, never a generated action
	// description, provider explanation or local draft annotation.
	Comment string `json:"comment,omitempty"`
}
