package api

// The words of the files in Browse (docs/plans/117-one-browse-view.md): the media list is the pictures, the videos
// and the recordings, drawn as tiles; the documents are every other file (a PDF, a text), drawn as rows. add-file
// keeps a file of any kind.

// mediaCounts counts the live files of each media kind in the scope of the media list.
type mediaCounts struct {
	Image int `json:"image"`
	Video int `json:"video"`
	Audio int `json:"audio"`
}

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
	c, _ := n.(float64)
	switch c {
	case 0:
		return "no " + many
	case 1:
		return "1 " + one
	}
	return num(c) + " " + many
}
