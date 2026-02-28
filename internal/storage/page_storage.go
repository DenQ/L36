package storage

import (
	"encoding/json"
	// "fmt"
	"l36/internal/models"
	"os"

	"github.com/google/uuid"

	// "path/filepath"
	"bufio"
	// "strings"
	"sync"
	"time"
)

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

	vid := uuid.New().String()

	newVersion := models.Version{
		ID:        vid,
		Content:   content,
		CreatedAt: time.Now().Unix(),
		IsLatest:  true,
	}

	newPage := models.Page{
		ID:       pid,
		Versions: []models.Version{newVersion},
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

func (s *PageStorage) AddVersion(pid string, content any) (models.Version, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, ok := s.pages[pid]
	if !ok {
		return models.Version{}, false
	}

	if len(page.Versions) > 0 {
		page.Versions[len(page.Versions)-1].IsLatest = false
	}

	newVer := models.Version{
		ID:        uuid.New().String(),
		ParentID:  page.Versions[len(page.Versions)-1].ID, // Ссылка на предка
		Content:   content,
		CreatedAt: time.Now().Unix(),
		IsLatest:  true,
	}

	page.Versions = append(page.Versions, newVer)

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
	if !ok {
		return models.Version{}, false
	}

	for _, v := range page.Versions {
		if v.ID == vid {
			return v, true
		}
	}

	return models.Version{}, false
}

func (s *PageStorage) SetLatest(pid string, vid string) (models.Version, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, ok := s.pages[pid]
	if !ok {
		return models.Version{}, false
	}

	var targetVersion *models.Version

	for i := range page.Versions {
		if page.Versions[i].ID == vid {
			targetVersion = &page.Versions[i]
		}
		page.Versions[i].IsLatest = false
	}

	if targetVersion != nil {
		targetVersion.IsLatest = true
		return *targetVersion, true
	}

	return models.Version{}, false
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
