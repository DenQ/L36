package models

type Version struct {
	ID        string `json:"versionId"`
	ParentID  string `json:"parentId,omitempty"`
	Content   string `json:"content,omitempty"`
	Patch     string `json:"patch,omitempty"`
	CreatedAt int64  `json:"dt"`
	IsLatest  bool   `json:"latest"`
	// Snapshot is true when this version keeps its full text, including an empty page.
	// A cleared version has neither the flag nor the text; restore rebuilds it from a descendant.
	Snapshot bool `json:"snapshot,omitempty"`

	// OnDisk is set once this version's body has been appended to the shard log.
	OnDisk bool `json:"-"`
	// Spilled means Content and Patch were dropped from memory and have to be read back.
	Spilled bool `json:"-"`
	// BodyOff and BodyLen locate the body inside the shard log.
	BodyOff int64 `json:"-"`
	BodyLen int   `json:"-"`
	// BodyHasContent is true when the log record still stores the full text.
	// Snapshots keep it. A superseded leaf keeps it only until the log is rewritten.
	BodyHasContent bool `json:"-"`
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
