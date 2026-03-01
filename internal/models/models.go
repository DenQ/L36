package models

type Version struct {
	ID        string `json:"versionId"`
	ParentID  string `json:"parentId,omitempty"`
	Content   string `json:"content"`
	Patch     string `json:"patch,omitempty"`
	CreatedAt int64  `json:"dt"`
	IsLatest  bool   `json:"latest"`
}

type Page struct {
	ID          string    `json:"pageId"`
	Versions    []Version `json:"versions"`
	LatestIndex int       `json:"latestIndex"`
}
