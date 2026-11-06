package main

import (
	"fmt"
	"io"
	"strings"

	"l36/internal/models"
)

type treeNode struct {
	version models.Version
	kids    []*treeNode
}

func writePage(w io.Writer, page models.Page) {
	fmt.Fprintln(w, page.ID)
	roots := forest(page.Versions)
	for i, n := range roots {
		writeNode(w, n, "", i == len(roots)-1)
	}
}

func forest(versions []models.Version) []*treeNode {
	byID := make(map[string]*treeNode, len(versions))
	order := make([]*treeNode, 0, len(versions))
	for _, v := range versions {
		n := &treeNode{version: v}
		byID[v.ID] = n
		order = append(order, n)
	}
	var roots []*treeNode
	for _, n := range order {
		parent := byID[n.version.ParentID]
		if n.version.ParentID == "" || parent == nil {
			roots = append(roots, n)
			continue
		}
		parent.kids = append(parent.kids, n)
	}
	return roots
}

func writeNode(w io.Writer, n *treeNode, prefix string, last bool) {
	branch := "├─ "
	childPrefix := prefix + "│  "
	if last {
		branch = "└─ "
		childPrefix = prefix + "   "
	}
	fmt.Fprintf(w, "%s%s%s\n", prefix, branch, label(n.version))
	for i, kid := range n.kids {
		writeNode(w, kid, childPrefix, i == len(n.kids)-1)
	}
}

func label(v models.Version) string {
	id := v.ID
	if v.IsLatest {
		id += " *"
	}
	text := oneLine(v.Content, 48)
	if text == "" {
		return id
	}
	return id + "  " + text
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
