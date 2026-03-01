package storage

import (
	"encoding/json"
	"l36/internal/models"
	"os"

	"github.com/google/uuid"
	"github.com/sergi/go-diff/diffmatchpatch"

	"bufio"
	"sync"
	"time"
)

var dmp = diffmatchpatch.New()

type Shard struct {
	mu    sync.RWMutex
	pages map[string]*models.Page
}

type PageStorage struct {
	shards [36]*Shard
}

func NewPageStorage() *PageStorage {
	ps := &PageStorage{}
	for i := 0; i < 36; i++ {
		ps.shards[i] = &Shard{
			pages: make(map[string]*models.Page),
		}
	}
	return ps
}

var GPageStorage = NewPageStorage()

func (s *PageStorage) getShard(pid string) *Shard {
	if len(pid) == 0 {
		return s.shards[0]
	}
	char := pid[0]
	var idx int

	switch {
	case char >= '0' && char <= '9':
		idx = int(char - '0')
	case char >= 'a' && char <= 'z':
		idx = int(char - 'a' + 10)
	case char >= 'A' && char <= 'Z':
		idx = int(char - 'A' + 10)
	default:
		idx = 0
	}
	return s.shards[idx%36]
}

func (s *PageStorage) CreatePage(pid string, content any) models.Page {
	shard := s.getShard(pid)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// if page exist return her. Else better htrow error...
	if page, ok := shard.pages[pid]; ok {
		return *page
	}

	var contentStr string
	if content == nil {
		contentStr = ""
	} else {
		switch v := content.(type) {
		case string:
			contentStr = v
		default:
			b, err := json.Marshal(v)

			if err != nil || string(b) == "null" {
				contentStr = ""
			} else {
				contentStr = string(b)
			}
		}
	}

	if contentStr == "null" {
		contentStr = ""
	}

	vid := uuid.New().String()

	newVersion := models.Version{
		ID:        vid,
		Content:   contentStr,
		CreatedAt: time.Now().Unix(),
		IsLatest:  true,
	}

	newPage := models.Page{
		ID:          pid,
		Versions:    []models.Version{newVersion},
		LatestIndex: 0,
	}

	shard.pages[pid] = &newPage
	return newPage
}

func (s *PageStorage) DeletePage(pid string) bool {
	shard := s.getShard(pid)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.pages[pid]; ok {
		delete(shard.pages, pid)
		return true
	}
	return false
}

func (s *PageStorage) AddVersion(pid string, newContent string) (models.Version, bool) {
	shard := s.getShard(pid)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		return models.Version{}, false
	}

	lastIdx := len(page.Versions) - 1
	oldLatest := &page.Versions[lastIdx]

	diffs := dmp.DiffMain(newContent, oldLatest.Content, false)
	delta := dmp.DiffToDelta(diffs)

	oldLatest.Content = ""
	oldLatest.Patch = delta
	oldLatest.IsLatest = false

	newVer := models.Version{
		ID:        uuid.New().String(),
		ParentID:  oldLatest.ID,
		Content:   newContent,
		CreatedAt: time.Now().Unix(),
		IsLatest:  true,
	}

	page.Versions = append(page.Versions, newVer)

	page.LatestIndex = len(page.Versions) - 1

	return newVer, true
}

func (s *PageStorage) GetHistory(pid string) ([]models.Version, bool) {
	shard := s.getShard(pid)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	page, ok := shard.pages[pid]
	if !ok {
		return nil, false
	}

	history := make([]models.Version, len(page.Versions))
	copy(history, page.Versions)

	return history, true
}

func (s *PageStorage) GetVersion(pid string, vid string) (models.Version, bool) {
	shard := s.getShard(pid)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		return models.Version{}, false
	}

	var targetIdx int = -1
	for i, v := range page.Versions {
		if v.ID == vid {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		return models.Version{}, false
	}

	lastIdx := len(page.Versions) - 1
	if targetIdx == lastIdx {
		return page.Versions[targetIdx], true
	}

	currentText := page.Versions[lastIdx].Content

	for i := lastIdx; i > targetIdx; i-- {
		patchStr := page.Versions[i-1].Patch
		if patchStr == "" {
			continue
		}

		diffs, _ := dmp.DiffFromDelta(currentText, patchStr)
		currentText = dmp.DiffText2(diffs)
	}

	result := page.Versions[targetIdx]
	result.Content = currentText
	return result, true
}

func (s *PageStorage) SetLatest(pid string, vid string) (models.Version, bool) {
	shard := s.getShard(pid)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	page, ok := shard.pages[pid]
	if !ok {
		return models.Version{}, false
	}

	for i := range page.Versions {
		if page.Versions[i].ID == vid {
			page.Versions[page.LatestIndex].IsLatest = false

			page.LatestIndex = i

			page.Versions[i].IsLatest = true

			return page.Versions[i], true
		}
	}

	return models.Version{}, false
}

func (s *PageStorage) GetLatestVersion(pid string) (models.Version, bool) {
	shard := s.getShard(pid)
	shard.mu.RLock()
	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		shard.mu.RUnlock()
		return models.Version{}, false
	}
	targetVid := page.Versions[page.LatestIndex].ID
	shard.mu.RUnlock()

	return s.GetVersion(pid, targetVid)
}

func (s *PageStorage) Dump(filePath string) error {
	tmpFile := filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)

	for i := 0; i < 36; i++ {
		shard := s.shards[i]

		shard.mu.RLock()
		for _, page := range shard.pages {
			if err := encoder.Encode(page); err != nil {
				shard.mu.RUnlock()
				return err
			}
		}
		shard.mu.RUnlock()
	}

	if err := f.Sync(); err != nil {
		return err
	}
	f.Close()

	return os.Rename(tmpFile, filePath)
}

func (s *PageStorage) Load(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var page models.Page
		if err := json.Unmarshal(scanner.Bytes(), &page); err != nil {
			return err
		}

		shard := s.getShard(page.ID)
		shard.mu.Lock()
		shard.pages[page.ID] = &page
		shard.mu.Unlock()
	}
	return scanner.Err()
}
