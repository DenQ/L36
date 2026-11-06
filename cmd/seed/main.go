package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	for i := 1; i <= 12; i++ {
		if err := s.short(i); err != nil {
			return err
		}
	}
	fmt.Printf("seeded %s\n", s.base)
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

func (s *seeder) short(n int) error {
	id := fmt.Sprintf("seed-page-%02d", n)
	if _, err := s.reset(id, fmt.Sprintf("Страница %d, первая версия", n)); err != nil {
		return err
	}
	var fork string
	steps := 2 + n%4
	for i := 2; i <= steps; i++ {
		vid, err := s.add(id, fmt.Sprintf("Страница %d, версия %d", n, i))
		if err != nil {
			return err
		}
		if i == 2 {
			fork = vid
		}
	}
	if n%3 == 0 && fork != "" {
		if err := s.head(id, fork); err != nil {
			return err
		}
		if _, err := s.add(id, fmt.Sprintf("Страница %d, ветка от второй версии", n)); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) reset(id, content string) (string, error) {
	if err := s.call(http.MethodDelete, "/api/pages/"+id, nil, nil); err != nil {
		return "", err
	}
	var created page
	body := map[string]string{"pageId": id, "content": content}
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
	if err := s.call(http.MethodPost, "/api/pages/"+id+"/versions", map[string]string{"content": content}, &created); err != nil {
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
