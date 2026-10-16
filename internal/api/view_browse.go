package api

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The folds of Browse (docs/plans/118-fold-and-find.md): the category tree folds each branch, a list in title order
// starts with its categories as folders, and a long list is in groups by its first letter. All of it is drawn from
// the props of GET /browse, so the resource and the catalog do not change.

const (
	letterGroupsFrom = 31  // a list in title order with this many rows is in letter groups
	lettersClosedAt  = 101 // with this many rows, the groups start closed: the letters are an index
)

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
		current = num(in["id"])
	}
	byPath := map[string]*treeNode{}
	var top []*treeNode
	for i, x := range cats {
		c, _ := x.(map[string]any)
		path, _ := c["path"].(string)
		title, _ := c["title"].(string)
		n := &treeNode{Title: title, Count: num(c["count"]), Current: current != "" && num(c["id"]) == current}
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

// browseList is the list of Browse as the page draws it: the categories as folders, then the rows, alone or in
// letter groups.
type browseList struct {
	Folders []map[string]any // id, title, href and count of each folder
	Rows    []any            // the rows, when they are not grouped
	Letters []letterGroup    // the rows, in groups by letter
	Closed  bool             // the groups start closed
}

// letterGroup is the rows whose titles start with one letter. Anchor is the id of its first row's list, which the
// letter bar links.
type letterGroup struct {
	Letter, Anchor string
	Rows           []any
}

// BrowseList splits the rows of Browse (docs/plans/118-fold-and-find.md, rules 2 and 3). On Pages in Everything the
// folders are the top categories of the tree, and no category is a row; inside a category, on All and on Pages,
// they are the categories among the rows. A list in title order with letterGroupsFrom rows is grouped by letter.
func (v *view) BrowseList() browseList {
	props, _ := v.Props.(map[string]any)
	rows, _ := props["rows"].([]any)
	kind, _ := props["kind"].(string)
	ghosts, _ := props["ghosts"].(bool)
	unfiled, _ := props["unfiled"].(bool)
	in, _ := props["in"].(map[string]any)
	cats, _ := props["categories"].([]any)
	links := v.Embedded("category")

	tree := map[string]map[string]any{} // a category of the tree by id: its title, href, count and depth
	for i, x := range cats {
		c, _ := x.(map[string]any)
		path, _ := c["path"].(string)
		f := map[string]any{"id": c["id"], "title": c["title"], "count": num(c["count"]), "top": !strings.Contains(path, "/")}
		if i < len(links) {
			f["href"] = links[i].Href
		}
		tree[num(c["id"])] = f
	}

	var l browseList
	switch {
	case kind == "page" && !ghosts && !unfiled && in == nil:
		for _, x := range cats {
			c, _ := x.(map[string]any)
			if f := tree[num(c["id"])]; f["top"] == true {
				l.Folders = append(l.Folders, f)
			}
		}
		for _, x := range rows {
			if r, _ := x.(map[string]any); r["category"] != true {
				l.Rows = append(l.Rows, x)
			}
		}
	case in != nil && (kind == "all" || kind == "page"):
		for _, x := range rows {
			r, _ := x.(map[string]any)
			if r["category"] != true {
				l.Rows = append(l.Rows, x)
				continue
			}
			f := tree[num(r["id"])]
			if f == nil { // a category the tree does not draw: nothing live is filed in it
				f = map[string]any{"id": r["id"], "title": r["title"], "count": "0", "href": browseHref(map[string][]string{"in": {r["title"].(string)}})}
			}
			l.Folders = append(l.Folders, f)
		}
	default:
		l.Rows = rows
	}

	byTitle := !ghosts && kind != "media" && kind != "document"
	if !byTitle || len(l.Rows) < letterGroupsFrom {
		return l
	}
	at := map[string]int{}
	for _, x := range l.Rows {
		r, _ := x.(map[string]any)
		title, _ := r["title"].(string)
		k := letterOf(title)
		i, ok := at[k]
		if !ok {
			i = len(l.Letters)
			at[k] = i
			l.Letters = append(l.Letters, letterGroup{Letter: k, Anchor: "letter-" + strconv.Itoa(i+1)})
		}
		l.Letters[i].Rows = append(l.Letters[i].Rows, x)
	}
	l.Closed = len(l.Rows) >= lettersClosedAt
	l.Rows = nil
	return l
}

// letterOf is the group of a title: its first letter in upper case, or "#" when it does not start with a letter.
func letterOf(title string) string {
	r, _ := utf8.DecodeRuneInString(title)
	if !unicode.IsLetter(r) {
		return "#"
	}
	return string(unicode.ToUpper(r))
}

// Mentions says in words how often the log mentions a person or a place. mentions and lastDay are the values of a
// row of the list as the template sees them: a JSON number and a day, or nothing.
func (v *view) Mentions(mentions, lastDay any) string {
	n, _ := mentions.(float64)
	if n < 1 {
		return "not mentioned yet"
	}
	words := "mentioned on " + count(n, "page", "pages")
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
	return count(n, one, many)
}
