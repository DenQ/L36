package models

type Version struct {
	ID        string `json:"versionId"`
	ParentID  string `json:"parentId,omitempty"`
	Content   string `json:"content,omitempty"`
	Patch     string `json:"patch,omitempty"`
	CreatedAt int64  `json:"dt"`
	IsLatest  bool   `json:"latest"`
	// Snapshot is true when Content is this version's full text, including an empty page.
	// A cleared version has neither the flag nor the text; restore rebuilds it from a descendant.
	Snapshot bool `json:"snapshot,omitempty"`
}

// VersionListItem is one node in a flat history list. The client builds the tree from parentId.
type VersionListItem struct {
	ID        string `json:"versionId"`
	ParentID  string `json:"parentId,omitempty"`
	CreatedAt int64  `json:"dt"`
	IsLatest  bool   `json:"latest"`
}

type Page struct {
	ID          string    `json:"pageId"`
	Versions    []Version `json:"versions"`
	LatestIndex int       `json:"latestIndex"`
}
