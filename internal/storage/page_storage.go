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

type PageStorage struct {
	mu    sync.RWMutex
	pages map[string]*models.Page
}

var GPageStorage = &PageStorage{
	pages: make(map[string]*models.Page),
}

func (s *PageStorage) CreatePage(pid string, content any) models.Page {
	s.mu.Lock()
	defer s.mu.Unlock()

	// if page exist return her. Else better htrow error...
	if page, ok := s.pages[pid]; ok {
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

	s.pages[pid] = &newPage
	return newPage
}

func (s *PageStorage) DeletePage(pid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.pages[pid]; ok {
		delete(s.pages, pid)
		return true
	}
	return false
}

func (s *PageStorage) AddVersion(pid string, newContent string) (models.Version, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, ok := s.pages[pid]
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	page, ok := s.pages[pid]
	if !ok {
		return nil, false
	}

	history := make([]models.Version, len(page.Versions))
	copy(history, page.Versions)

	return history, true
}

func (s *PageStorage) GetVersion(pid string, vid string) (models.Version, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	page, ok := s.pages[pid]
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
	s.mu.Lock()
	defer s.mu.Unlock()

	page, ok := s.pages[pid]
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	page, ok := s.pages[pid]
	if !ok || len(page.Versions) == 0 {
		return models.Version{}, false
	}

	targetVid := page.Versions[page.LatestIndex].ID

	return s.GetVersion(pid, targetVid)
}

func (s *PageStorage) Dump(filePath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tmpFile := filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)

	for _, page := range s.pages {
		if err := encoder.Encode(page); err != nil {
			return err
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}
	f.Close()

	return os.Rename(tmpFile, filePath)
}

func (s *PageStorage) Load(filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		var page models.Page
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &page); err != nil {
			return err
		}
		s.pages[page.ID] = &page
	}

	return scanner.Err()
}
