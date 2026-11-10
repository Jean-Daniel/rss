// Package opml reads and writes OPML subscription lists, the format feed
// readers use to move subscriptions from one to another.
package opml

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// List is what an OPML file holds.
type List struct {
	Feeds []Entry
	// Groups are the folders at the top level, in order, empty ones
	// included.
	Groups []string
}

// Entry is a feed listed in an OPML file.
type Entry struct {
	// Group is the title of the outline the feed is in, empty at the top
	// level. Deeper folders count as the folder they are in at the top.
	Group   string
	Title   string
	URL     string
	SiteURL string
}

type outline struct {
	Attrs    []xml.Attr `xml:",any,attr"`
	Outlines []outline  `xml:"outline"`
}

// attr reads an attribute, whatever its case: exporters disagree on
// xmlUrl, xmlURL and xmlurl.
func (o *outline) attr(name string) string {
	for _, a := range o.Attrs {
		if strings.EqualFold(a.Name.Local, name) {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

func (o *outline) title() string {
	if t := o.attr("title"); t != "" {
		return t
	}
	return o.attr("text")
}

var (
	ErrNotOPML  = errors.New("The file is not an OPML subscription list.")
	errEncoding = errors.New("The file must be encoded in UTF-8.")
)

// readErr remembers a failure to read, so it is not taken for bad XML.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF {
		e.err = err
	}
	return n, err
}

// Parse reads the feeds and folders of an OPML file, in order. A failure to
// read r is returned as it is.
func Parse(r io.Reader) (*List, error) {
	var doc struct {
		XMLName xml.Name
		Body    struct {
			Outlines []outline `xml:"outline"`
		} `xml:"body"`
	}
	src := &readErr{r: r}
	var badEncoding bool
	d := xml.NewDecoder(src)
	// Files written by hand or by old tools use HTML entities and leave
	// tags open.
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(name string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(name) {
		case "utf-8", "utf8", "us-ascii", "ascii":
			return input, nil
		}
		if cm := legacyCharsets[strings.ToLower(name)]; cm != nil {
			return cm.NewDecoder().Reader(input), nil
		}
		badEncoding = true
		return nil, errEncoding
	}
	err := d.Decode(&doc)
	switch {
	case src.err != nil:
		return nil, src.err
	case badEncoding:
		return nil, errEncoding
	case err != nil || !strings.EqualFold(doc.XMLName.Local, "opml"):
		return nil, ErrNotOPML
	}
	list := &List{}
	var walk func(outlines []outline, group string)
	walk = func(outlines []outline, group string) {
		for i := range outlines {
			o := &outlines[i]
			if u := o.attr("xmlUrl"); u != "" {
				list.Feeds = append(list.Feeds, Entry{Group: group, Title: o.title(), URL: u, SiteURL: o.attr("htmlUrl")})
			} else if group == "" && o.title() != "" {
				list.Groups = append(list.Groups, o.title())
			}
			if len(o.Outlines) > 0 {
				inner := group
				if inner == "" {
					inner = o.title()
				}
				walk(o.Outlines, inner)
			}
		}
	}
	walk(doc.Body.Outlines, "")
	return list, nil
}

// legacyCharsets are the encodings besides UTF-8 that subscription lists
// from older readers use.
var legacyCharsets = map[string]*charmap.Charmap{
	"iso-8859-1":   charmap.ISO8859_1,
	"latin1":       charmap.ISO8859_1,
	"iso-8859-15":  charmap.ISO8859_15,
	"windows-1252": charmap.Windows1252,
	"cp1252":       charmap.Windows1252,
}

// Group is a group of feeds to write.
type Group struct {
	Title string
	Feeds []Entry
}

type outlineOut struct {
	XMLName  xml.Name     `xml:"outline"`
	Text     string       `xml:"text,attr"`
	Title    string       `xml:"title,attr,omitempty"`
	Type     string       `xml:"type,attr,omitempty"`
	XMLURL   string       `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string       `xml:"htmlUrl,attr,omitempty"`
	Outlines []outlineOut `xml:"outline"`
}

func feedOutline(e Entry) outlineOut {
	return outlineOut{Text: e.Title, Title: e.Title, Type: "rss", XMLURL: e.URL, HTMLURL: e.SiteURL}
}

// Write writes an OPML 2.0 file: the groups, each with its feeds, then the
// feeds that are in no group.
func Write(w io.Writer, title string, created time.Time, groups []Group, ungrouped []Entry) error {
	type head struct {
		Title       string `xml:"title"`
		DateCreated string `xml:"dateCreated"`
	}
	// The body is written even when empty: readers expect one.
	type body struct {
		Outlines []outlineOut `xml:"outline"`
	}
	doc := struct {
		XMLName xml.Name `xml:"opml"`
		Version string   `xml:"version,attr"`
		Head    head     `xml:"head"`
		Body    body     `xml:"body"`
	}{
		Version: "2.0",
		Head:    head{Title: title, DateCreated: created.UTC().Format(time.RFC1123Z)},
	}
	for _, g := range groups {
		o := outlineOut{Text: g.Title, Title: g.Title}
		for _, e := range g.Feeds {
			o.Outlines = append(o.Outlines, feedOutline(e))
		}
		doc.Body.Outlines = append(doc.Body.Outlines, o)
	}
	for _, e := range ungrouped {
		doc.Body.Outlines = append(doc.Body.Outlines, feedOutline(e))
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
