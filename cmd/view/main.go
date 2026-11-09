package main

import (
	"flag"
	"fmt"
	"os"

	"l36/internal/storage"
)

func main() {
	n := flag.Int("n", -1, "shard number, 0..35")
	dir := flag.String("dir", "data", "directory with shard files")
	flag.Parse()

	if *n < 0 || *n > 35 {
		fmt.Fprintln(os.Stderr, "usage: make view N=0")
		os.Exit(2)
	}

	store := storage.NewPageStorage()
	if err := store.Load(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "view: %v\n", err)
		os.Exit(1)
	}
	pages := store.ShardPages(*n)
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
