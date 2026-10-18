package view

import "strings"

// The folds of Browse (docs/plans/118-fold-and-find.md): the category tree folds each branch. It is drawn from the
// props of GET /browse, so the resource and the catalog do not change.

// treeNode is a branch of the category tree: the category, its link, its count, and the categories under it. Open
// says that the current branch is this one or is under it.
type treeNode struct {
	Title, Href, Count string
	Current, Open      bool
	Kids               []*treeNode
}

// BrowseTree is the category tree of Browse as branches: each category under the one its path names, in the order
// of the paths.
func (v *view) BrowseTree() []*treeNode {
	props, _ := v.Props.(map[string]any)
	cats, _ := props["categories"].([]any)
	links := v.Embedded("category")
	current := ""
	if in, _ := props["in"].(map[string]any); in != nil {
		current = Num(in["id"])
	}
	byPath := map[string]*treeNode{}
	var top []*treeNode
	for i, x := range cats {
		c, _ := x.(map[string]any)
		path, _ := c["path"].(string)
		title, _ := c["title"].(string)
		n := &treeNode{Title: title, Count: Num(c["count"]), Current: current != "" && Num(c["id"]) == current}
		if i < len(links) {
			n.Href = links[i].Href
		}
		byPath[path] = n
		parent := ""
		if j := strings.LastIndex(path, "/"); j >= 0 {
			parent = path[:j]
		}
		if p := byPath[parent]; p != nil && parent != "" {
			p.Kids = append(p.Kids, n)
		} else {
			top = append(top, n)
		}
	}
	var open func(n *treeNode) bool
	open = func(n *treeNode) bool {
		for _, k := range n.Kids {
			if open(k) {
				n.Open = true
			}
		}
		if n.Current && len(n.Kids) > 0 {
			n.Open = true
		}
		return n.Current || n.Open
	}
	for _, n := range top {
		open(n)
	}
	return top
}

// Mentions says in words how often the log mentions a person or a place. mentions and lastDay are the values of a
// row of the list as the template sees them: a JSON number and a day, or nothing.
func (v *view) Mentions(mentions, lastDay any) string {
	n, _ := mentions.(float64)
	if n < 1 {
		return "not mentioned yet"
	}
	words := "mentioned on " + Count(n, "page", "pages")
	if day, _ := lastDay.(string); day != "" {
		words += ", last " + v.clock().day(day)
	}
	return words
}

// The words of the files in Browse (docs/plans/117-one-browse-view.md): the media list is the pictures, the videos
// and the recordings, drawn as tiles; the documents are every other file (a PDF, a text), drawn as rows. add-file
// keeps a file of any kind.

// FileWords says what a row of a list of files is, and when: "picture · Wed 14 May", "recording · no day yet",
// "application/pdf · Mon 12 May" (a file of another kind is named by its type).
func (v *view) FileWords(kind, mime, day any) string {
	words, ok := map[string]string{"image": "picture", "video": "video", "audio": "recording"}[kind.(string)]
	if !ok {
		words, _ = mime.(string)
	}
	if d, _ := day.(string); d != "" {
		return words + " · " + v.clock().day(d)
	}
	return words + " · no day yet"
}

// MediaCount says a count of the media list in words: "3 pictures", "1 video", "no recordings".
func (v *view) MediaCount(n any, one, many string) string {
	if c, _ := n.(float64); c == 0 {
		return "no " + many
	}
	return Count(n, one, many)
}
