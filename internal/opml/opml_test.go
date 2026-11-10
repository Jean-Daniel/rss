package opml

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="1.0">
<head><title>My feeds</title></head>
<body>
  <outline text="Loose &amp; free" type="rss" xmlUrl="https://loose.example/feed" htmlUrl="https://loose.example/"/>
  <outline text="Tech" title="Tech">
    <outline text="Blog&nbsp;one" xmlURL="https://one.example/rss"/>
    <outline title="Deeper">
      <outline text="Two" xmlurl=" https://two.example/atom "/>
    </outline>
  </outline>
  <outline text="Empty folder"></outline>
  <outline type="rss"/>
</body>
</opml>`
	got, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := &List{
		Feeds: []Entry{
			{Title: "Loose & free", URL: "https://loose.example/feed", SiteURL: "https://loose.example/"},
			{Group: "Tech", Title: "Blog\u00a0one", URL: "https://one.example/rss"},
			{Group: "Tech", Title: "Two", URL: "https://two.example/atom"},
		},
		Groups: []string{"Tech", "Empty folder"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse =\n%+v\nwant\n%+v", got, want)
	}

	for _, bad := range []string{"", "not xml at all", `<rss version="2.0"><channel></channel></rss>`} {
		if _, err := Parse(strings.NewReader(bad)); err != ErrNotOPML {
			t.Errorf("Parse(%q) error = %v, want ErrNotOPML", bad, err)
		}
	}
	latin1 := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><opml><body>" +
		"<outline text=\"Caf\xe9\" xmlUrl=\"https://cafe.example/rss\"/></body></opml>"
	if list, err := Parse(strings.NewReader(latin1)); err != nil || len(list.Feeds) != 1 || list.Feeds[0].Title != "Café" {
		t.Errorf("Parse of an ISO-8859-1 file: %+v %v", list, err)
	}
	sjis := `<?xml version="1.0" encoding="Shift_JIS"?><opml><body></body></opml>`
	if _, err := Parse(strings.NewReader(sjis)); err != errEncoding {
		t.Errorf("Parse of a Shift_JIS file: error = %v", err)
	}
}

func TestWriteParse(t *testing.T) {
	groups := []Group{
		{Title: "News & <Politics>", Feeds: []Entry{{Title: "Daily \"News\"", URL: "https://news.example/rss?a=1&b=2", SiteURL: "https://news.example/"}}},
		{Title: "Empty"},
	}
	loose := []Entry{{Title: "Loose", URL: "https://loose.example/feed"}}
	var buf bytes.Buffer
	if err := Write(&buf, "Fever subscriptions", time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), groups, loose); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, part := range []string{`<?xml version="1.0" encoding="UTF-8"?>`, `<opml version="2.0">`, `<dateCreated>Tue, 10 Nov 2026 12:00:00 +0000</dateCreated>`} {
		if !strings.Contains(out, part) {
			t.Errorf("output lacks %s:\n%s", part, out)
		}
	}
	got, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	want := &List{
		Feeds: []Entry{
			{Group: "News & <Politics>", Title: "Daily \"News\"", URL: "https://news.example/rss?a=1&b=2", SiteURL: "https://news.example/"},
			{Title: "Loose", URL: "https://loose.example/feed"},
		},
		Groups: []string{"News & <Politics>", "Empty"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, want)
	}

	buf.Reset()
	if err := Write(&buf, "Nothing", time.Now(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "<body></body>") {
		t.Errorf("empty list without a body:\n%s", buf.String())
	}
}
