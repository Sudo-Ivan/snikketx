package linkpreview

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Field length caps so a hostile page cannot stuff unbounded strings into a
// preview reply.
const (
	maxTitleLen = 300
	maxDescLen  = 600
	maxSiteLen  = 200
	maxFieldURL = 2048
)

// parseMetadata extracts OpenGraph tags from an HTML document read from r.
// page is the final URL the document was fetched from and is used to resolve
// relative references and to default the canonical URL and favicon.
func parseMetadata(r io.Reader, page *url.URL) *Metadata {
	m := &Metadata{URL: page.String()}

	doc, err := html.Parse(r)
	if err != nil {
		return m
	}

	// ogTitle and ogDesc win over the plain tag fallbacks no matter where
	// in the document each appears.
	var ogTitle, ogDesc, title, desc, image, icon string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "meta":
				key := attrOf(n, "property")
				if key == "" {
					key = attrOf(n, "name")
				}
				content := strings.TrimSpace(attrOf(n, "content"))
				switch strings.ToLower(key) {
				case "og:title":
					if ogTitle == "" {
						ogTitle = content
					}
				case "og:description":
					if ogDesc == "" {
						ogDesc = content
					}
				case "description":
					if desc == "" {
						desc = content
					}
				case "og:site_name":
					if m.SiteName == "" {
						m.SiteName = content
					}
				case "og:image", "og:image:url", "og:image:secure_url":
					if image == "" {
						image = content
					}
				case "og:url":
					if canonical := resolveAgainst(page, content); canonical != "" && m.URL == page.String() {
						m.URL = canonical
					}
				}
			case "link":
				if icon == "" && relHasToken(n, "icon") {
					icon = attrOf(n, "href")
				}
			case "title":
				if title == "" {
					title = textOf(n)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	m.Title = clamp(firstNonEmpty(ogTitle, title), maxTitleLen)
	m.Description = clamp(firstNonEmpty(ogDesc, desc), maxDescLen)
	m.SiteName = clamp(m.SiteName, maxSiteLen)
	m.Image = resolveAgainst(page, image)
	m.Favicon = resolveAgainst(page, icon)
	if m.Favicon == "" {
		m.Favicon = page.Scheme + "://" + page.Host + "/favicon.ico"
	}
	return m
}

// attrOf returns the named attribute of an element node.
func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// relHasToken reports whether the rel attribute of a link element contains
// the given token.
func relHasToken(n *html.Node, token string) bool {
	for _, rel := range strings.Fields(strings.ToLower(attrOf(n, "rel"))) {
		if rel == token {
			return true
		}
	}
	return false
}

// textOf returns the text content of a node subtree.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(c *html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// resolveAgainst turns raw into an absolute http or https URL relative to
// base. Anything else returns an empty string so javascript: or data:
// references never reach the client.
func resolveAgainst(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxFieldURL {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

func clamp(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
