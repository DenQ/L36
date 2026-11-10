package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type version struct {
	ID       string `json:"versionId"`
	ParentID string `json:"parentId"`
	Content  string `json:"content"`
	Latest   bool   `json:"latest"`
}

type page struct {
	ID       string    `json:"pageId"`
	Versions []version `json:"versions"`
}

func main() {
	base := os.Getenv("L36_URL")
	if base == "" {
		base = "http://127.0.0.1:1236"
	}
	base = strings.TrimRight(base, "/")

	client := &http.Client{Timeout: 10 * time.Second}
	seed := &seeder{base: base, client: client}

	if err := seed.run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

type seeder struct {
	base   string
	client *http.Client
}

func (s *seeder) run() error {
	if err := s.tree(); err != nil {
		return err
	}
	if err := s.notes(); err != nil {
		return err
	}
	if err := s.drafts(); err != nil {
		return err
	}
	pages, versions, err := s.bulk()
	if err != nil {
		return err
	}
	fmt.Printf("seeded %s (%d bulk pages, %d versions, ~%d versions per shard)\n", s.base, pages, versions, versions/36)
	return nil
}

// tree is the picture: A → B → C, head moves to B, then BA grows from B, then CA from C.
func (s *seeder) tree() error {
	const id = "seed-tree"
	a, err := s.reset(id, "A — корень")
	if err != nil {
		return err
	}
	b, err := s.add(id, "B — основная линия")
	if err != nil {
		return err
	}
	if _, err = s.add(id, "C — хвост до отката"); err != nil {
		return err
	}
	if err = s.head(id, b); err != nil {
		return err
	}
	if _, err = s.add(id, "BA — ветка от B"); err != nil {
		return err
	}
	if err = s.head(id, a); err != nil {
		return err
	}
	_, err = s.add(id, "AA — ветка от корня")
	return err
}

func (s *seeder) notes() error {
	const id = "seed-notes"
	if _, err := s.reset(id, "Заметка. Черновик 1"); err != nil {
		return err
	}
	lines := []string{
		"Заметка. Добавил контекст",
		"Заметка. Убрал вступление",
		"Заметка. Пример с деревом версий",
		"Заметка. Список для клиента без текста",
		"Заметка. Финал линейной ветки",
	}
	var prev string
	for i, line := range lines {
		vid, err := s.add(id, line)
		if err != nil {
			return err
		}
		if i == 2 {
			prev = vid
		}
	}
	if err := s.head(id, prev); err != nil {
		return err
	}
	_, err := s.add(id, "Заметка. Ответвление после третьей правки")
	return err
}

func (s *seeder) drafts() error {
	const id = "seed-drafts"
	if _, err := s.reset(id, "Черновик спецификации"); err != nil {
		return err
	}
	var mid string
	for i := 2; i <= 5; i++ {
		vid, err := s.add(id, fmt.Sprintf("Черновик, правка %d", i))
		if err != nil {
			return err
		}
		if i == 3 {
			mid = vid
		}
	}
	if err := s.head(id, mid); err != nil {
		return err
	}
	if _, err := s.add(id, "Черновик, альтернатива от правки 3"); err != nil {
		return err
	}
	_, err := s.add(id, "Черновик, продолжение альтернативы")
	return err
}

// bulk writes three pages onto every shard. Version counts are 50, 100 and 150,
// so a page averages 100 versions. pageId is chosen so fnv32a matches storage.shardIndex.
func (s *seeder) bulk() (int, int, error) {
	counts := []int{50, 100, 150}
	pages := 0
	versions := 0
	for shard := 0; shard < 36; shard++ {
		for p, n := range counts {
			id, err := idForShard(shard, p)
			if err != nil {
				return pages, versions, err
			}
			if err := s.history(id, n); err != nil {
				return pages, versions, err
			}
			pages++
			versions += n
		}
	}
	return pages, versions, nil
}

func (s *seeder) history(id string, n int) error {
	if _, err := s.reset(id, fmt.Sprintf("%s, версия 1", id)); err != nil {
		return err
	}
	for i := 2; i <= n; i++ {
		if _, err := s.add(id, fmt.Sprintf("%s, версия %d", id, i)); err != nil {
			return err
		}
	}
	return nil
}

func idForShard(shard, page int) (string, error) {
	for k := 0; k < 10000; k++ {
		id := fmt.Sprintf("seed-s%02d-p%d-%d", shard, page, k)
		if shardOf(id) == shard {
			return id, nil
		}
	}
	return "", fmt.Errorf("no page id for shard %d page %d", shard, page)
}

func shardOf(pageID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(pageID))
	return int(h.Sum32() % 36)
}

func (s *seeder) reset(id, content string) (string, error) {
	if err := s.call(http.MethodDelete, "/api/pages/"+id, nil, nil); err != nil {
		return "", err
	}
	var created page
	body := map[string]string{"pageId": id, "content": content, "userId": "seed"}
	if err := s.call(http.MethodPost, "/api/pages", body, &created); err != nil {
		return "", err
	}
	if len(created.Versions) == 0 {
		return "", fmt.Errorf("create %s: empty history", id)
	}
	fmt.Printf("page %s\n", id)
	return created.Versions[0].ID, nil
}

func (s *seeder) add(id, content string) (string, error) {
	var created version
	if err := s.call(http.MethodPost, "/api/pages/"+id+"/versions", map[string]string{"content": content, "userId": "seed"}, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (s *seeder) head(id, vid string) error {
	return s.call(http.MethodPost, "/api/pages/"+id+"/versions/"+vid+"/latest", nil, nil)
}

func (s *seeder) call(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if method == http.MethodDelete && (resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound) {
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(payload)))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
	}
	return nil
}
