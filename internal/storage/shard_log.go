package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"l36/internal/models"
)

// A shard log is an append-only sequence of frames:
//
//	uint32 header length | header JSON | uint32 body length | body JSON
//
// A snapshot body stores the full text. Every other version stores only its patch.
// Delete and set-latest frames have an empty body.
// BodyOff on a version points at the body JSON. A rewrite of the log assigns new offsets.

type logHeader struct {
	Op         string `json:"op"`
	PageID     string `json:"pageId"`
	VersionID  string `json:"versionId,omitempty"`
	ParentID   string `json:"parentId,omitempty"`
	CreatedAt  int64  `json:"dt,omitempty"`
	UserID     string `json:"userId,omitempty"`
	IsLatest   bool   `json:"latest,omitempty"`
	Snapshot   bool   `json:"snapshot,omitempty"`
	HasContent bool   `json:"text,omitempty"`
}

type versionBody struct {
	Content string `json:"content,omitempty"`
	Patch   string `json:"patch,omitempty"`
}

type diskOp struct {
	header  logHeader
	body    versionBody
	hasBody bool
}

func (s *Shard) enqueue(op diskOp) {
	s.pending = append(s.pending, op)
	s.gen++
}

type preparedOp struct {
	header     logHeader
	body       versionBody
	hasBody    bool
	hasContent bool
}

func (s *Shard) flush() error {
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	path := s.path
	prepared := make([]preparedOp, len(batch))
	for i, op := range batch {
		prepared[i] = s.prepareOp(op)
	}
	s.mu.Unlock()

	if path == "" {
		if len(batch) == 0 {
			return nil
		}
		s.mu.Lock()
		s.pending = append(batch, s.pending...)
		s.mu.Unlock()
		return fmt.Errorf("shard path is not set")
	}

	type marked struct {
		pageID     string
		verID      string
		off        int64
		n          int
		hasContent bool
	}
	var marks []marked
	written := 0
	var writeErr error

	if len(batch) > 0 {
		if err := s.ensureFile(); err != nil {
			writeErr = err
		} else {
			for _, op := range prepared {
				off, n, err := s.appendPrepared(op)
				if err != nil {
					writeErr = err
					break
				}
				written++
				if op.hasBody {
					marks = append(marks, marked{op.header.PageID, op.header.VersionID, off, n, op.hasContent})
				}
			}
			if written > 0 {
				s.fileMu.Lock()
				err := s.file.Sync()
				s.fileMu.Unlock()
				if err != nil && writeErr == nil {
					writeErr = err
				}
			}
		}
	}

	s.mu.Lock()
	if written < len(batch) {
		s.pending = append(batch[written:], s.pending...)
	}
	for _, m := range marks {
		s.markOnDisk(m.pageID, m.verID, m.off, m.n, m.hasContent)
	}
	if writeErr != nil {
		s.mu.Unlock()
		return writeErr
	}
	s.stripAll()
	if !s.needsCompact() {
		s.mu.Unlock()
		return nil
	}
	pages, gen, src := s.snapshotForCompact()
	s.mu.Unlock()
	if err := s.compact(pages, gen, src); err != nil {
		log.Printf("shard compact: %v", err)
	}
	return nil
}

// prepareOp copies the body as it should be stored. A snapshot keeps its text.
// Any other version keeps only the patch, which is all that is left in memory
// once a child has been added.
func (s *Shard) prepareOp(op diskOp) preparedOp {
	out := preparedOp{header: op.header, body: op.body, hasBody: op.hasBody}
	if !op.hasBody {
		return out
	}
	page := s.pages[op.header.PageID]
	if page == nil {
		return out
	}
	idx := versionIndex(page, op.header.VersionID)
	if idx < 0 {
		return out
	}
	v := page.Versions[idx]
	if v.Snapshot {
		out.body = versionBody{Content: v.Content, Patch: v.Patch}
		out.hasContent = true
		out.header.HasContent = true
		return out
	}
	out.header.HasContent = false
	out.body = versionBody{Patch: v.Patch}
	out.hasContent = false
	return out
}

func (s *Shard) markOnDisk(pageID, versionID string, off int64, n int, hasContent bool) {
	page := s.pages[pageID]
	if page == nil {
		return
	}
	idx := versionIndex(page, versionID)
	if idx < 0 {
		return
	}
	v := &page.Versions[idx]
	v.OnDisk = true
	v.BodyOff = off
	v.BodyLen = n
	v.BodyHasContent = hasContent
}

// needsCompact reports that ten versions still store a full text they no longer
// need. The log is rewritten then, so a page does not keep a copy per version.
func (s *Shard) needsCompact() bool {
	n := 0
	for _, page := range s.pages {
		for i := range page.Versions {
			v := &page.Versions[i]
			if v.OnDisk && v.BodyHasContent && !v.Snapshot {
				n++
			}
		}
	}
	return n >= snapshotEvery
}

type compactPage struct {
	id       string
	latestID string
	versions []models.Version
}

func (s *Shard) snapshotForCompact() ([]compactPage, uint64, *os.File) {
	pages := make([]compactPage, 0, len(s.pages))
	for _, page := range s.pages {
		cp := compactPage{
			id:       page.ID,
			versions: append([]models.Version(nil), page.Versions...),
		}
		if validLatest(page) {
			cp.latestID = page.Versions[page.LatestIndex].ID
		}
		pages = append(pages, cp)
	}
	return pages, s.gen, s.file
}

type placedBody struct {
	pageID     string
	verID      string
	off        int64
	n          int
	hasContent bool
}

func (s *Shard) compact(pages []compactPage, gen uint64, src *os.File) error {
	if src != nil {
		for pi := range pages {
			for vi := range pages[pi].versions {
				v := &pages[pi].versions[vi]
				if !v.Spilled || v.BodyLen == 0 {
					continue
				}
				buf, err := readAt(src, v.BodyOff, v.BodyLen)
				if err != nil {
					return err
				}
				var body versionBody
				if err := json.Unmarshal(buf, &body); err != nil {
					return err
				}
				v.Content = body.Content
				v.Patch = body.Patch
			}
		}
	}

	tmp := s.path + ".compact"
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	var pos int64
	var placed []placedBody
	for _, page := range pages {
		if len(page.versions) == 0 {
			continue
		}
		for i, v := range page.versions {
			op := "add"
			if i == 0 {
				op = "create"
			}
			body := versionBody{Patch: v.Patch}
			hasContent := v.Snapshot
			if hasContent {
				body.Content = v.Content
			}
			header := logHeader{
				Op:         op,
				PageID:     page.id,
				VersionID:  v.ID,
				ParentID:   v.ParentID,
				CreatedAt:  v.CreatedAt,
				UserID:     v.UserID,
				IsLatest:   i == len(page.versions)-1 && v.ID == page.latestID,
				Snapshot:   v.Snapshot,
				HasContent: hasContent,
			}
			off, n, err := writeFrame(f, pos, header, body, true)
			if err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			pos = off + int64(n)
			placed = append(placed, placedBody{page.id, v.ID, off, n, hasContent})
		}
		last := page.versions[len(page.versions)-1]
		if page.latestID != "" && page.latestID != last.ID {
			header := logHeader{Op: "latest", PageID: page.id, VersionID: page.latestID}
			off, n, err := writeFrame(f, pos, header, versionBody{}, false)
			if err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			pos = off + int64(n)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		f.Close()
		os.Remove(tmp)
		return nil
	}
	if err := os.Rename(tmp, s.path); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	for _, p := range placed {
		s.markOnDisk(p.pageID, p.verID, p.off, p.n, p.hasContent)
	}
	s.fileMu.Lock()
	old := s.file
	s.file = f
	if old != nil {
		if s.fileRefs == 0 {
			old.Close()
		} else {
			s.retired = append(s.retired, old)
		}
	}
	s.fileMu.Unlock()
	return nil
}

func readAt(f *os.File, off int64, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := f.ReadAt(buf, off)
	return buf, err
}

func (s *Shard) stripAll() {
	for _, page := range s.pages {
		stripCold(page)
		s.warmHot(page)
	}
}

// warmHot pulls spilled bodies on the hot tail back into memory.
func (s *Shard) warmHot(page *models.Page) {
	hot := hotSet(page)
	for i := range page.Versions {
		v := &page.Versions[i]
		if !hot[v.ID] || !v.Spilled {
			continue
		}
		if err := s.loadBody(v); err != nil {
			return
		}
		v.Spilled = false
	}
}

// stripCold drops text and patches that sit strictly above the hot tail.
// The hot tail is the latest version plus its ancestors up to and including
// the nearest snapshot. Bodies that are not on disk yet stay in memory.
func stripCold(page *models.Page) {
	hot := hotSet(page)
	for i := range page.Versions {
		v := &page.Versions[i]
		if hot[v.ID] || !v.OnDisk {
			continue
		}
		v.Content = ""
		v.Patch = ""
		v.Spilled = true
	}
}

// hotSet is the path from the latest version back to the nearest snapshot.
// The latest node itself is kept even though a leaf is a snapshot.
func hotSet(page *models.Page) map[string]bool {
	hot := make(map[string]bool)
	if !validLatest(page) {
		return hot
	}
	id := page.Versions[page.LatestIndex].ID
	seen := make(map[string]bool)
	first := true
	for id != "" && !seen[id] {
		seen[id] = true
		idx := versionIndex(page, id)
		if idx < 0 {
			break
		}
		hot[id] = true
		if !first && page.Versions[idx].Snapshot {
			break
		}
		first = false
		id = page.Versions[idx].ParentID
	}
	return hot
}

func (s *Shard) ensureFile() error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if s.file != nil {
		return nil
	}
	f, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return err
	}
	s.file = f
	return nil
}

func (s *Shard) appendPrepared(op preparedOp) (int64, int, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	frameAt, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, 0, err
	}
	return writeFrame(s.file, frameAt, op.header, op.body, op.hasBody)
}

func writeFrame(f *os.File, frameAt int64, header logHeader, body versionBody, hasBody bool) (int64, int, error) {
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return 0, 0, err
	}
	var bodyBytes []byte
	if hasBody {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return 0, 0, err
		}
	}
	if len(headerBytes) > maxShardLineSize || len(bodyBytes) > maxShardLineSize {
		return 0, 0, fmt.Errorf("shard record exceeds %d bytes", maxShardLineSize)
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(headerBytes)))
	if _, err := f.Write(lenBuf[:]); err != nil {
		return 0, 0, err
	}
	if _, err := f.Write(headerBytes); err != nil {
		return 0, 0, err
	}
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(bodyBytes)))
	if _, err := f.Write(lenBuf[:]); err != nil {
		return 0, 0, err
	}
	bodyOff := frameAt + 4 + int64(len(headerBytes)) + 4
	if len(bodyBytes) > 0 {
		if _, err := f.Write(bodyBytes); err != nil {
			return 0, 0, err
		}
	}
	return bodyOff, len(bodyBytes), nil
}

func (s *Shard) replay() error {
	f, err := os.OpenFile(s.path, os.O_RDWR, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.file = f
	if err := s.readLog(f); err != nil {
		return err
	}
	if err := s.finishReplay(); err != nil {
		return err
	}
	_, err = f.Seek(0, io.SeekEnd)
	return err
}

func (s *Shard) readLog(f *os.File) error {
	var pos int64
	for {
		frameAt := pos
		header, bodyOff, bodyLen, next, err := readFrame(f, &pos, maxShardLineSize)
		if err == io.EOF {
			return nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return f.Truncate(frameAt)
		}
		if err != nil {
			return err
		}
		if err := s.applyHeader(header, bodyOff, bodyLen); err != nil {
			return err
		}
		pos = next
	}
}

func readFrame(r io.Reader, pos *int64, max int) (logHeader, int64, int, int64, error) {
	var hdrLenBuf [4]byte
	if _, err := io.ReadFull(r, hdrLenBuf[:]); err != nil {
		return logHeader{}, 0, 0, *pos, err
	}
	*pos += 4
	hdrLen := int(binary.BigEndian.Uint32(hdrLenBuf[:]))
	if hdrLen > max {
		return logHeader{}, 0, 0, *pos, fmt.Errorf("shard record exceeds %d bytes", max)
	}
	headerBytes := make([]byte, hdrLen)
	if _, err := io.ReadFull(r, headerBytes); err != nil {
		return logHeader{}, 0, 0, *pos, err
	}
	*pos += int64(hdrLen)

	var bodyLenBuf [4]byte
	if _, err := io.ReadFull(r, bodyLenBuf[:]); err != nil {
		return logHeader{}, 0, 0, *pos, err
	}
	*pos += 4
	bodyLen := int(binary.BigEndian.Uint32(bodyLenBuf[:]))
	if bodyLen > max {
		return logHeader{}, 0, 0, *pos, fmt.Errorf("shard record exceeds %d bytes", max)
	}
	bodyOff := *pos
	if bodyLen > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(bodyLen)); err != nil {
			return logHeader{}, 0, 0, *pos, err
		}
		*pos += int64(bodyLen)
	}

	var header logHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return logHeader{}, 0, 0, *pos, err
	}
	return header, bodyOff, bodyLen, *pos, nil
}

func (s *Shard) applyHeader(h logHeader, bodyOff int64, bodyLen int) error {
	switch h.Op {
	case "create":
		if _, ok := s.pages[h.PageID]; ok {
			return fmt.Errorf("page %s created twice", h.PageID)
		}
		s.pages[h.PageID] = &models.Page{
			ID:          h.PageID,
			Versions:    []models.Version{versionFromHeader(h, bodyOff, bodyLen)},
			LatestIndex: 0,
		}
	case "add":
		page := s.pages[h.PageID]
		if page == nil {
			return fmt.Errorf("add on missing page %s", h.PageID)
		}
		for i := range page.Versions {
			page.Versions[i].IsLatest = false
		}
		page.Versions = append(page.Versions, versionFromHeader(h, bodyOff, bodyLen))
		page.LatestIndex = len(page.Versions) - 1
	case "latest":
		page := s.pages[h.PageID]
		if page == nil {
			return fmt.Errorf("latest on missing page %s", h.PageID)
		}
		idx := versionIndex(page, h.VersionID)
		if idx < 0 {
			return fmt.Errorf("latest %s missing on %s", h.VersionID, h.PageID)
		}
		for i := range page.Versions {
			page.Versions[i].IsLatest = false
		}
		page.Versions[idx].IsLatest = true
		page.LatestIndex = idx
	case "delete":
		delete(s.pages, h.PageID)
	default:
		return fmt.Errorf("unknown shard op %q", h.Op)
	}
	return nil
}

func versionFromHeader(h logHeader, bodyOff int64, bodyLen int) models.Version {
	return models.Version{
		ID:             h.VersionID,
		ParentID:       h.ParentID,
		CreatedAt:      h.CreatedAt,
		UserID:         h.UserID,
		IsLatest:       h.IsLatest,
		Snapshot:       h.Snapshot,
		OnDisk:         true,
		BodyOff:        bodyOff,
		BodyLen:        bodyLen,
		BodyHasContent: h.HasContent,
	}
}

func (s *Shard) finishReplay() error {
	for _, page := range s.pages {
		recomputeSnapshots(page)
		hot := hotSet(page)
		for i := range page.Versions {
			v := &page.Versions[i]
			if !hot[v.ID] {
				v.Spilled = true
				v.Content = ""
				v.Patch = ""
				continue
			}
			if err := s.loadBody(v); err != nil {
				return err
			}
			v.Spilled = false
		}
	}
	return nil
}

func recomputeSnapshots(page *models.Page) {
	kids := childIndexes(page)
	for i := range page.Versions {
		v := &page.Versions[i]
		v.Snapshot = anchorSnapshot(page, v.ID) || len(kids[v.ID]) == 0
	}
}

func (s *Shard) loadBody(v *models.Version) error {
	if v.BodyLen == 0 {
		return nil
	}
	buf, err := s.readPayload(v.BodyOff, v.BodyLen)
	if err != nil {
		return err
	}
	var body versionBody
	if err := json.Unmarshal(buf, &body); err != nil {
		return err
	}
	v.Content = body.Content
	v.Patch = body.Patch
	return nil
}

func (s *Shard) readPayload(off int64, n int) ([]byte, error) {
	s.fileMu.Lock()
	f := s.file
	if f == nil {
		s.fileMu.Unlock()
		return nil, fmt.Errorf("shard file is not open")
	}
	s.fileRefs++
	s.fileMu.Unlock()
	defer s.releaseFile()

	buf := make([]byte, n)
	_, err := f.ReadAt(buf, off)
	return buf, err
}

func (s *Shard) releaseFile() {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	s.fileRefs--
	if s.fileRefs == 0 {
		for _, old := range s.retired {
			old.Close()
		}
		s.retired = nil
	}
}

func (s *Shard) fillSpilled(nodes []models.Version) error {
	for i := range nodes {
		if !nodes[i].Spilled {
			continue
		}
		if err := s.loadBody(&nodes[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *PageStorage) StartFlusher(every time.Duration, stop <-chan struct{}) {
	s.flushWG.Add(len(s.shards))
	for i := range s.shards {
		go func(shard *Shard) {
			defer s.flushWG.Done()
			ticker := time.NewTicker(every)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := shard.flush(); err != nil {
						log.Printf("shard flush: %v", err)
					}
				case <-stop:
					if err := shard.flush(); err != nil {
						log.Printf("shard flush: %v", err)
					}
					return
				}
			}
		}(s.shards[i])
	}
}

func (s *PageStorage) FlushDone() {
	s.flushWG.Wait()
}
