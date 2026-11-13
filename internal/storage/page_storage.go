package storage

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"sync"
	"time"

	"l36/internal/models"

	"github.com/google/uuid"
	"github.com/sergi/go-diff/diffmatchpatch"
)

var dmp = diffmatchpatch.New()

type Shard struct {
	mu        sync.RWMutex
	pages     map[string]*models.Page
	diffMu    sync.RWMutex
	diffCache map[uint64]string

	path     string
	file     *os.File
	fileMu   sync.Mutex
	fileRefs int
	retired  []*os.File
	pending  []diskOp
	gen      uint64
}

type PageStorage struct {
	shards  [36]*Shard
	flushWG sync.WaitGroup
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

const maxUserIDLen = 256

var (
	ErrEmptyPageID     = errors.New("empty page id")
	ErrPageExists      = errors.New("page already exists")
	ErrPageNotFound    = errors.New("page not found")
	ErrEmptyUserID     = errors.New("empty user id")
	ErrUserIDTooLong   = errors.New("user id too long")
	ErrVersionNotFound = errors.New("version not found")
	ErrCorruptPatch    = errors.New("corrupt patch")
)

func checkUserID(id string) error {
	if id == "" {
		return ErrEmptyUserID
	}
	if len(id) > maxUserIDLen {
		return ErrUserIDTooLong
	}
	return nil
}

func (s *PageStorage) CreatePage(pid string, content string, userID string) (models.Page, error) {
	if pid == "" {
		return models.Page{}, ErrEmptyPageID
	}
	if err := checkUserID(userID); err != nil {
		return models.Page{}, err
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
		UserID:    userID,
		IsLatest:  true,
		Snapshot:  true,
		Depth:     1,
	}

	newPage := models.Page{
		ID:          pid,
		Versions:    []models.Version{newVersion},
		LatestIndex: 0,
	}

	shard.pages[pid] = &newPage
	shard.enqueue(diskOp{
		header: logHeader{
			Op:        "create",
			PageID:    pid,
			VersionID: vid,
			CreatedAt: newVersion.CreatedAt,
			UserID:    newVersion.UserID,
			IsLatest:  true,
			Snapshot:  true,
		},
		hasBody: true,
		body:    versionBody{Content: content},
	})
	return newPage, nil
}

func (s *PageStorage) DeletePage(pid string) bool {
	shard := s.getShard(pid)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.pages[pid]; ok {
		shard.enqueue(diskOp{header: logHeader{Op: "delete", PageID: pid}})
		delete(shard.pages, pid)
		return true
	}
	return false
}

func (s *PageStorage) AddVersion(pid string, newContent string, userID string) (models.Version, error) {
	if err := checkUserID(userID); err != nil {
		return models.Version{}, err
	}

	shard := s.getShard(pid)

	for {
		shard.mu.RLock()
		page, ok := shard.pages[pid]
		if !ok || len(page.Versions) == 0 || !validLatest(page) {
			shard.mu.RUnlock()
			return models.Version{}, ErrPageNotFound
		}
		head := page.Versions[page.LatestIndex]
		headID := head.ID
		oldContent := head.Content
		// The head keeps its text. SetLatest can move onto a version whose text was cleared.
		needRestore := head.Spilled || !head.Snapshot
		shard.mu.RUnlock()

		if needRestore {
			var err error
			oldContent, err = shard.versionContent(pid, headID)
			if err != nil {
				return models.Version{}, err
			}
		}

		delta := shard.diff(oldContent, newContent)

		shard.mu.Lock()
		page, ok = shard.pages[pid]
		if !ok || len(page.Versions) == 0 || !validLatest(page) {
			shard.mu.Unlock()
			return models.Version{}, ErrPageNotFound
		}
		if page.Versions[page.LatestIndex].ID != headID {
			shard.mu.Unlock()
			continue
		}

		parent := &page.Versions[page.LatestIndex]
		parent.IsLatest = false
		if anchorSnapshot(parent) {
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
			UserID:    userID,
			IsLatest:  true,
			Snapshot:  true,
			Depth:     parent.Depth + 1,
		}
		page.Versions = append(page.Versions, newVer)
		page.LatestIndex = len(page.Versions) - 1
		shard.enqueue(diskOp{
			header: logHeader{
				Op:        "add",
				PageID:    pid,
				VersionID: newVer.ID,
				ParentID:  newVer.ParentID,
				CreatedAt: newVer.CreatedAt,
				UserID:    newVer.UserID,
				IsLatest:  true,
				Snapshot:  true,
			},
			hasBody: true,
			body:    versionBody{Content: newVer.Content, Patch: newVer.Patch},
		})
		shard.mu.Unlock()

		newVer.Patch = ""
		newVer.Snapshot = false
		return newVer, nil
	}
}

func validLatest(page *models.Page) bool {
	return page.LatestIndex >= 0 && page.LatestIndex < len(page.Versions)
}

func (s *Shard) versionContent(pid, vid string) (string, error) {
	restored, err := s.restoreVersion(pid, vid)
	if err != nil {
		return "", err
	}
	return restored.Content, nil
}

func (s *Shard) restoreVersion(pid, vid string) (models.Version, error) {
	s.mu.RLock()
	page, ok := s.pages[pid]
	if !ok || len(page.Versions) == 0 {
		s.mu.RUnlock()
		return models.Version{}, ErrVersionNotFound
	}
	nodes, err := chainCopies(page, vid)
	s.mu.RUnlock()
	if err != nil {
		return models.Version{}, err
	}
	if err := s.fillSpilled(nodes); err != nil {
		return models.Version{}, err
	}
	return applyCopiedChain(nodes)
}

func chainCopies(page *models.Page, vid string) ([]models.Version, error) {
	if versionIndex(page, vid) < 0 {
		return nil, ErrVersionNotFound
	}
	base := nearestBase(page, vid)
	if base < 0 {
		return nil, fmt.Errorf("%w: no snapshot under %s", ErrCorruptPatch, vid)
	}
	var nodes []models.Version
	cur := base
	for {
		nodes = append(nodes, page.Versions[cur])
		if page.Versions[cur].ID == vid {
			return nodes, nil
		}
		if len(nodes) > len(page.Versions) {
			return nil, fmt.Errorf("%w: cycle under %s", ErrCorruptPatch, vid)
		}
		parent := versionIndex(page, page.Versions[cur].ParentID)
		if parent < 0 {
			return nil, fmt.Errorf("%w: missing parent of %s", ErrCorruptPatch, page.Versions[cur].ID)
		}
		cur = parent
	}
}

func applyCopiedChain(nodes []models.Version) (models.Version, error) {
	text := nodes[0].Content
	for i := 0; i < len(nodes)-1; i++ {
		next, err := applyReverse(text, nodes[i].Patch)
		if err != nil {
			return models.Version{}, err
		}
		text = next
	}
	result := nodes[len(nodes)-1]
	result.Content = text
	result.Patch = ""
	result.Snapshot = false
	result.Spilled = false
	return result, nil
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
	return s.getShard(pid).restoreVersion(pid, vid)
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
// Depth is counted from 1 at the root, so a chain of ten versions anchors the tenth.
func anchorSnapshot(v *models.Version) bool {
	return v.ParentID == "" || v.Depth%snapshotEvery == 0
}

// assignDepths fills Depth from parent links. Parents are stored ahead of their children.
func assignDepths(page *models.Page) {
	index := make(map[string]int, len(page.Versions))
	for i := range page.Versions {
		v := &page.Versions[i]
		index[v.ID] = i
		if v.ParentID == "" {
			v.Depth = 1
			continue
		}
		parent, ok := index[v.ParentID]
		if !ok {
			v.Depth = 1
			continue
		}
		v.Depth = page.Versions[parent].Depth + 1
	}
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

	page, ok := shard.pages[pid]
	if !ok {
		shard.mu.Unlock()
		return models.Version{}, false
	}

	found := false
	for i := range page.Versions {
		if page.Versions[i].ID != vid {
			continue
		}
		if page.LatestIndex >= 0 && page.LatestIndex < len(page.Versions) {
			page.Versions[page.LatestIndex].IsLatest = false
		}
		page.LatestIndex = i
		page.Versions[i].IsLatest = true
		shard.enqueue(diskOp{header: logHeader{Op: "latest", PageID: pid, VersionID: vid}})
		found = true
		break
	}
	shard.mu.Unlock()
	if !found {
		return models.Version{}, false
	}

	restored, err := shard.restoreVersion(pid, vid)
	if err != nil {
		return models.Version{}, false
	}
	return restored, true
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

// maxShardLineSize is the largest log frame Load will accept.
const maxShardLineSize = 64 << 20

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

func (s *PageStorage) bindDir(dataDir string) {
	for i := range s.shards {
		s.shards[i].path = fmt.Sprintf("%s/shard_%d.log", dataDir, i)
	}
}

// Dump appends pending operations to the shard logs, syncs them, and spills
// version bodies that sit outside the hot tail.
func (s *PageStorage) Dump(dataDir string) error {
	if dataDir != "" {
		s.bindDir(dataDir)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 36)

	for i := range s.shards {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := s.shards[idx].flush(); err != nil {
				errs <- fmt.Errorf("shard %d flush failed: %w", idx, err)
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
	s.bindDir(dataDir)
	var wg sync.WaitGroup
	errs := make(chan error, 36)

	start := time.Now()
	for i := range s.shards {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := s.shards[idx].replay(); err != nil {
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

func (s *PageStorage) ShardPages(n int) []models.Page {
	if n < 0 || n >= len(s.shards) {
		return nil
	}
	shard := s.shards[n]
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	out := make([]models.Page, 0, len(shard.pages))
	for _, page := range shard.pages {
		cp := *page
		cp.Versions = append([]models.Version(nil), page.Versions...)
		out = append(out, cp)
	}
	return out
}
