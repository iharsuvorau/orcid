package orcid

import (
	"fmt"
	"html/template"
	"log"
	"net/url"
	"strings"
)

func unescape(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

// UpdateExternalIDsURL populates a slice of works with an URI for
// external ids if its value is missing.
func UpdateExternalIDsURL(works []*Work) {
	var uri string
	for i, w := range works {
		for ii, id := range w.ExternalIDs {
			uri = ""
			switch id.Type {
			case "doi":
				if len(id.URL) > 0 {
					uri = string(id.URL)
				} else if len(id.Value) > 0 {
					uri = fmt.Sprintf("http://doi.org/%s", id.Value)
				} else {
					continue
				}
				uri = unescape(uri)
				u, err := url.Parse(uri)
				if err != nil {
					log.Fatal(err)
				}
				uri = u.String()
				works[i].DoiURI = template.HTML(uri)
			case "eid":
				// TODO: is there a way to generate
				// freely fetchable record from
				// scopus?
			default:
				// if not implemented, skip the assignment
				continue
			}
			uri = unescape(uri)
			u, err := url.Parse(uri)
			if err != nil {
				log.Fatal(err)
			}
			uri = u.String()
			works[i].ExternalIDs[ii].URL = template.HTML(uri)
		}

	}
}

// UpdateContributorsLine populates a slice of works with an URI for
// external ids if its value is missing.
func UpdateContributorsLine(works []*Work) {
	for i, w := range works {
		contribs := make([]string, len(w.Contributors))
		for ii, c := range w.Contributors {
			contribs[ii] = c.Name
		}

		// formatting of contributors is according to
		// https://research.moreheadstate.edu/c.php?g=107001&p=695197
		works[i].ContributorsLine = strings.Join(contribs, ", ")
	}
}

// UpdateMarkup is a tricky function and relies on the underlying
// template which is passed by a client. So the client must be aware
// of what is going on here to effectively render works.
func UpdateMarkup(works []*Work) {
	for i, w := range works {
		// we escape the whole title using <nowiki></nowiki>
		// but we do want {{sub|}} to be parsed by the wiki
		works[i].Title = template.HTML(
			strings.ReplaceAll(
				strings.ReplaceAll(
					strings.ReplaceAll(
						strings.ReplaceAll(
							strings.ReplaceAll(
								strings.ReplaceAll(string(w.Title), "<inf>", "</nowiki>{{sub|"),
								"</inf>", "}}<nowiki>"),
							"&lt;inf&gt;", "</nowiki>{{sub|"),
						"&lt;/inf&gt;", "}}<nowiki>"),
					"<sup>", "</nowiki>{{sup|"),
				"</sup>", "}}<nowiki>"),
		)
		contribs := make([]string, len(w.Contributors))
		for ii, c := range w.Contributors {
			contribs[ii] = c.Name
		}

		// formatting of contributors is according to
		// https://research.moreheadstate.edu/c.php?g=107001&p=695197
		works[i].ContributorsLine = strings.Join(contribs, ", ")
	}
}
