package models

type Version struct {
	ID        string `json:"versionId"`
	ParentID  string `json:"parentId,omitempty"`
	Content   any    `json:"content"` // any, так как там JSON
	CreatedAt int64  `json:"dt"`
	IsLatest  bool   `json:"latest"`
}

type Page struct {
	ID       string    `json:"pageId"`
	Versions []Version `json:"versions"` // В истории храним всё
}
