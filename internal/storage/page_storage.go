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

// snapshotEvery is the longest gap between versions that keep their full text.
// The root and every leaf keep theirs as well, so a restore walks at most this many patches.
const snapshotEvery = 10

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
		Snapshot:  true,
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

	for {
		shard.mu.RLock()
		page, ok := shard.pages[pid]
		if !ok || len(page.Versions) == 0 || !validLatest(page) {
			shard.mu.RUnlock()
			return models.Version{}, false
		}
		head := page.Versions[page.LatestIndex]
		headID := head.ID
		oldContent, err := headContent(page, headID)
		shard.mu.RUnlock()
		if err != nil {
			return models.Version{}, false
		}

		delta := shard.diff(oldContent, newContent)

		shard.mu.Lock()
		page, ok = shard.pages[pid]
		if !ok || len(page.Versions) == 0 || !validLatest(page) {
			shard.mu.Unlock()
			return models.Version{}, false
		}
		if page.Versions[page.LatestIndex].ID != headID {
			shard.mu.Unlock()
			continue
		}

		parent := &page.Versions[page.LatestIndex]
		parent.IsLatest = false
		if anchorSnapshot(page, parent.ID) {
			parent.Snapshot = true
		} else {
			parent.Content = ""
			parent.Snapshot = false
		}

		newVer := models.Version{
			ID:        uuid.New().String(),
			ParentID:  parent.ID,
			Content:   newContent,
			Patch:     delta,
			CreatedAt: time.Now().Unix(),
			IsLatest:  true,
			Snapshot:  true,
		}
		page.Versions = append(page.Versions, newVer)
		page.LatestIndex = len(page.Versions) - 1
		shard.mu.Unlock()

		newVer.Patch = ""
		newVer.Snapshot = false
		return newVer, true
	}
}

func validLatest(page *models.Page) bool {
	return page.LatestIndex >= 0 && page.LatestIndex < len(page.Versions)
}

func headContent(page *models.Page, id string) (string, error) {
	restored, err := restoreLocked(page, id)
	if err != nil {
		return "", err
	}
	return restored.Content, nil
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
	version, err := restoreLocked(page, vid)
	if err != nil {
		return models.Version{}, err
	}
	version.Patch = ""
	version.Snapshot = false
	return version, nil
}

func restoreLocked(page *models.Page, vid string) (models.Version, error) {
	target := versionIndex(page, vid)
	if target < 0 {
		return models.Version{}, ErrVersionNotFound
	}

	base := nearestBase(page, vid)
	if base < 0 {
		return models.Version{}, fmt.Errorf("%w: no snapshot under %s", ErrCorruptPatch, vid)
	}
	if base == target {
		return page.Versions[target], nil
	}

	text := page.Versions[base].Content
	cur := base
	for steps := 0; page.Versions[cur].ID != vid; steps++ {
		if steps > len(page.Versions) {
			return models.Version{}, fmt.Errorf("%w: cycle under %s", ErrCorruptPatch, vid)
		}
		next, err := applyReverse(text, page.Versions[cur].Patch)
		if err != nil {
			return models.Version{}, err
		}
		text = next
		parent := versionIndex(page, page.Versions[cur].ParentID)
		if parent < 0 {
			return models.Version{}, fmt.Errorf("%w: missing parent of %s", ErrCorruptPatch, page.Versions[cur].ID)
		}
		cur = parent
	}

	result := page.Versions[target]
	result.Content = text
	return result, nil
}

// anchorSnapshot reports whether this version keeps its full text after a child is added.
// The root always does. Further down a path, every snapshotEvery-th version does.
func anchorSnapshot(page *models.Page, id string) bool {
	idx := versionIndex(page, id)
	if idx < 0 {
		return false
	}
	if page.Versions[idx].ParentID == "" {
		return true
	}
	return versionDepth(page, id)%snapshotEvery == 0
}

func versionDepth(page *models.Page, id string) int {
	depth := 0
	seen := make(map[string]bool)
	for id != "" && !seen[id] {
		seen[id] = true
		idx := versionIndex(page, id)
		if idx < 0 {
			break
		}
		depth++
		id = page.Versions[idx].ParentID
	}
	return depth
}

func versionIndex(page *models.Page, id string) int {
	for i := range page.Versions {
		if page.Versions[i].ID == id {
			return i
		}
	}
	return -1
}

func childIndexes(page *models.Page) map[string][]int {
	kids := make(map[string][]int)
	for i := range page.Versions {
		parent := page.Versions[i].ParentID
		if parent == "" {
			continue
		}
		kids[parent] = append(kids[parent], i)
	}
	return kids
}

// nearestBase is the closest version at or below id whose Content is stored in full.
// A snapshot qualifies, and so does a leaf, which always holds its own text.
func nearestBase(page *models.Page, id string) int {
	start := versionIndex(page, id)
	if start < 0 {
		return -1
	}
	kids := childIndexes(page)
	queue := []int{start}
	seen := make([]bool, len(page.Versions))
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if seen[idx] {
			continue
		}
		seen[idx] = true
		children := kids[page.Versions[idx].ID]
		if page.Versions[idx].Snapshot || len(children) == 0 {
			return idx
		}
		queue = append(queue, children...)
	}
	return -1
}

func applyReverse(text, patch string) (string, error) {
	if patch == "" {
		return text, nil
	}
	diffs, err := dmp.DiffFromDelta(text, patch)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrCorruptPatch, err.Error())
	}
	return dmp.DiffText2(diffs), nil
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
			if page.LatestIndex >= 0 && page.LatestIndex < len(page.Versions) {
				page.Versions[page.LatestIndex].IsLatest = false
			}
			page.LatestIndex = i
			page.Versions[i].IsLatest = true

			restored, err := restoreLocked(page, vid)
			if err != nil {
				return models.Version{}, false
			}
			restored.Patch = ""
			restored.Snapshot = false
			return restored, true
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
		migrateLinearPatches(&page)
		markSnapshots(&page)
		s.pages[page.ID] = &page
	}
}

// migrateLinearPatches rewrites a chain saved by the old layout, where the patch sat on the parent and only the tail held text.
func migrateLinearPatches(page *models.Page) {
	n := len(page.Versions)
	if n < 2 || page.Versions[n-1].Patch != "" {
		return
	}
	nonempty := 0
	earlierPatch := false
	for i, v := range page.Versions {
		if v.Content != "" {
			nonempty++
		}
		if i < n-1 && v.Patch != "" {
			earlierPatch = true
		}
	}
	if nonempty > 1 || !earlierPatch {
		return
	}
	for i := n - 1; i >= 1; i-- {
		page.Versions[i].Patch = page.Versions[i-1].Patch
	}
	page.Versions[0].Patch = ""
}

// markSnapshots flags versions that already store full text, so a shard written
// before the snapshot field still restores. An empty leaf counts: its text is "".
func markSnapshots(page *models.Page) {
	kids := childIndexes(page)
	for i := range page.Versions {
		v := &page.Versions[i]
		if v.Snapshot {
			continue
		}
		if v.Content != "" || len(kids[v.ID]) == 0 {
			v.Snapshot = true
		}
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
