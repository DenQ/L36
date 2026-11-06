package storage

import (
	"encoding/json"
	"fmt"
	"l36/internal/models"
	"os"

	"github.com/google/uuid"
	"github.com/sergi/go-diff/diffmatchpatch"

	"bufio"
	"errors"
	"hash/fnv"
	"io"
	"sync"
	"time"
)

var dmp = diffmatchpatch.New()

type Shard struct {
	mu        sync.RWMutex
	pages     map[string]*models.Page
	diffMu    sync.RWMutex
	diffCache map[uint64]string
}

type PageStorage struct {
	shards [36]*Shard
}

const maxDiffCacheEntries = 10000

func (s *Shard) diff(oldContent, newContent string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(oldContent))
	_, _ = h.Write([]byte(newContent))
	key := h.Sum64()

	s.diffMu.RLock()
	if result, ok := s.diffCache[key]; ok {
		s.diffMu.RUnlock()
		return result
	}
	s.diffMu.RUnlock()

	diffs := dmp.DiffMain(newContent, oldContent, true)
	result := dmp.DiffToDelta(diffs)

	s.diffMu.Lock()
	if s.diffCache == nil || len(s.diffCache) > maxDiffCacheEntries {
		s.diffCache = make(map[uint64]string)
	}
	s.diffCache[key] = result
	s.diffMu.Unlock()

	return result
}

func NewPageStorage() *PageStorage {
	ps := &PageStorage{}

	for i := 0; i < 36; i++ {
		ps.shards[i] = &Shard{
			pages:     make(map[string]*models.Page),
			diffCache: make(map[uint64]string),
		}
	}

	return ps
}

var GPageStorage = NewPageStorage()

func init() {
	dmp.DiffTimeout = time.Millisecond * 10
}

// shardIndex is a pure function: the same pageId always lands on the same shard.
func shardIndex(pid string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(pid))
	return int(h.Sum32() % 36)
}

func (s *PageStorage) getShard(pid string) *Shard {
	return s.shards[shardIndex(pid)]
}

var (
	ErrEmptyPageID     = errors.New("empty page id")
	ErrPageExists      = errors.New("page already exists")
	ErrVersionNotFound = errors.New("version not found")
	ErrCorruptPatch    = errors.New("corrupt patch")
)

func (s *PageStorage) CreatePage(pid string, content string) (models.Page, error) {
	if pid == "" {
		return models.Page{}, ErrEmptyPageID
	}

	shard := s.getShard(pid)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.pages[pid]; ok {
		return models.Page{}, ErrPageExists
	}

	vid := uuid.New().String()

	newVersion := models.Version{
		ID:        vid,
		Content:   content,
		CreatedAt: time.Now().Unix(),
		IsLatest:  true,
	}

	newPage := models.Page{
		ID:          pid,
		Versions:    []models.Version{newVersion},
		LatestIndex: 0,
	}

	shard.pages[pid] = &newPage
	return newPage, nil
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

	shard.mu.RLock()
	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		shard.mu.RUnlock()
		return models.Version{}, false
	}

	oldContent := page.Versions[len(page.Versions)-1].Content
	shard.mu.RUnlock()

	delta := shard.diff(oldContent, newContent)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	page, ok = shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		return models.Version{}, false
	}

	lastIdx := len(page.Versions) - 1
	oldLatest := &page.Versions[lastIdx]

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

func (s *PageStorage) GetVersion(pid string, vid string) (models.Version, error) {
	shard := s.getShard(pid)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		return models.Version{}, ErrVersionNotFound
	}

	var targetIdx int = -1
	for i, v := range page.Versions {
		if v.ID == vid {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		return models.Version{}, ErrVersionNotFound
	}

	lastIdx := len(page.Versions) - 1
	if targetIdx == lastIdx {
		return page.Versions[targetIdx], nil
	}

	currentText := page.Versions[lastIdx].Content

	for i := lastIdx; i > targetIdx; i-- {
		patchStr := page.Versions[i-1].Patch
		if patchStr == "" {
			continue
		}

		diffs, err := dmp.DiffFromDelta(currentText, patchStr)
		if err != nil {
			return models.Version{}, fmt.Errorf("%w: %s", ErrCorruptPatch, err.Error())
		}
		currentText = dmp.DiffText2(diffs)
	}

	result := page.Versions[targetIdx]
	result.Content = currentText
	return result, nil
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

func (s *PageStorage) GetLatestVersion(pid string) (models.Version, error) {
	shard := s.getShard(pid)
	shard.mu.RLock()
	page, ok := shard.pages[pid]
	if !ok || len(page.Versions) == 0 {
		shard.mu.RUnlock()
		return models.Version{}, ErrVersionNotFound
	}
	targetVid := page.Versions[page.LatestIndex].ID
	shard.mu.RUnlock()

	return s.GetVersion(pid, targetVid)
}

func (s *Shard) dump(filePath string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.pages) == 0 {
		return removeShardFiles(filePath)
	}

	tmpFile := filePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(f)
	for _, page := range s.pages {
		if err := encoder.Encode(page); err != nil {
			f.Close()
			return err
		}
	}
	f.Sync()
	f.Close()
	return os.Rename(tmpFile, filePath)
}

func removeShardFiles(filePath string) error {
	for _, path := range []string{filePath, filePath + ".tmp"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Shard) load(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readPages(f, maxShardLineSize)
}

// maxShardLineSize is the largest JSON line Load will accept.
// A longer line is an error. The page is not cut and stored in part.
const maxShardLineSize = 64 << 20

func (s *Shard) readPages(r io.Reader, maxLine int) error {
	br := bufio.NewReader(r)
	for {
		line, err := readShardLine(br, maxLine)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var page models.Page
		if err := json.Unmarshal(line, &page); err != nil {
			return err
		}
		s.pages[page.ID] = &page
	}
}

func readShardLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		frag, err := r.ReadSlice('\n')
		if len(frag) > 0 {
			if len(buf)+len(frag) > max {
				return nil, fmt.Errorf("shard line exceeds %d bytes", max)
			}
			buf = append(buf, frag...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(buf) == 0 {
				return nil, io.EOF
			}
			return trimLineEnding(buf), nil
		}
		if err != nil {
			return nil, err
		}
		return trimLineEnding(buf), nil
	}
}

func trimLineEnding(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if len(b) > 0 && b[len(b)-1] == '\r' {
		b = b[:len(b)-1]
	}
	return b
}

func (s *PageStorage) Dump(dataDir string) error {
	var wg sync.WaitGroup
	errs := make(chan error, 36)

	for i := 0; i < 36; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("%s/shard_%d.json", dataDir, idx)
			if err := s.shards[idx].dump(path); err != nil {
				errs <- fmt.Errorf("shard %d dump failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	if len(errs) > 0 {
		return <-errs
	}
	return nil
}

func (s *PageStorage) Load(dataDir string) error {
	var wg sync.WaitGroup
	errs := make(chan error, 36)

	start := time.Now()
	for i := 0; i < 36; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := fmt.Sprintf("%s/shard_%d.json", dataDir, idx)
			if err := s.shards[idx].load(path); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	fmt.Printf(" [L-36] LOAD COMPLETE: %v\n", time.Since(start))
	if len(errs) > 0 {
		return <-errs
	}
	return nil
}
