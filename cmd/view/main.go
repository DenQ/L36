package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"l36/internal/models"
)

func main() {
	n := flag.Int("n", -1, "shard number, 0..35")
	dir := flag.String("dir", "data", "directory with shard files")
	flag.Parse()

	if *n < 0 || *n > 35 {
		fmt.Fprintln(os.Stderr, "usage: make view N=0")
		os.Exit(2)
	}

	path := filepath.Join(*dir, fmt.Sprintf("shard_%d.json", *n))
	pages, err := loadPages(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("shard %d is empty\n", *n)
			return
		}
		fmt.Fprintf(os.Stderr, "view: %v\n", err)
		os.Exit(1)
	}
	if len(pages) == 0 {
		fmt.Printf("shard %d is empty\n", *n)
		return
	}
	for i, page := range pages {
		if i > 0 {
			fmt.Println()
		}
		writePage(os.Stdout, page)
	}
}

func loadPages(path string) ([]models.Page, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var pages []models.Page
	sc := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var page models.Page
		if err := json.Unmarshal(line, &page); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		pages = append(pages, page)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return pages, nil
}
