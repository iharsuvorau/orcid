// Package orcid provides an API for research works fetching using the
// ORCID Public HTTP API.
//
// Check the ORCID docs:
// https://members.orcid.org/api/about-public-api.
package orcid

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Client is the ORCID API client for requests handling.
type Client struct {
	apiBase *url.URL
}

// New return a client.
func New(apiBase string) (*Client, error) {
	// the leading slash is needed to resolve dependent URLs further
	apiBase = strings.TrimRight(apiBase, "/") + "/"

	u, err := url.Parse(apiBase)
	if err != nil {
		return nil, err
	}

	return &Client{apiBase: u}, nil
}

// ID is an ORCID.
type ID string

// IDFromURL creates ID from an URL.
func IDFromURL(s string) (ID, error) {
	if !strings.HasPrefix(s, "http") {
		s = "https://" + s
	}
	uri, err := url.Parse(s)
	if err != nil {
		return ID(""), err
	}
	id := strings.Trim(uri.Path, "/")
	return ID(id), nil
}

// IsEmpty checks if the ID is an empty string.
func (id ID) IsEmpty() bool {
	return string(id) == ""
}

// String returns ID.
func (id ID) String() string {
	return string(id)
}

// Activity is the ORCID <activities:works> XML element.
type Activity struct {
	XMLName xml.Name `xml:"works"`
	Works   []Work   `xml:"group>work-summary"`
}

// Work is the ORCID <work:work> and <work:work-summary> XML elements.
type Work struct {
	// Common

	Created    time.Time `xml:"created-date"`
	Modified   time.Time `xml:"last-modified-date"`
	SourceName string    `xml:"source>source-name"`
	Year       int       `xml:"publication-date>year"`
	Month      int       `xml:"publication-date>month"`
	Day        int       `xml:"publication-date>day"`

	// Work

	// Path to detailed information about the work.
	Path         string        `xml:"path,attr"`
	Title        template.HTML `xml:"title>title"`
	JournalTitle string        `xml:"journal-title"`
	Citation     *Citation     `xml:"citation"`
	Type         string        `xml:"type"`
	ExternalIDs  []struct {
		Type  string        `xml:"external-id-type"` // doi = Digital Object Identifier, eid = Scopus
		Value string        `xml:"external-id-value"`
		URL   template.HTML `xml:"external-id-url"`
	} `xml:"external-ids>external-id"`
	URI          string         `xml:"url"`
	Contributors []*Contributor `xml:"contributors>contributor"`

	// Convenience fields. Do not belong to the ORCID schema. Used in templates

	DoiURI           template.HTML
	ContributorsLine string
}

// ExternalIDValue returns ExternalID.Value by ExternalID.Type.
func (w *Work) ExternalIDValue(s string) string {
	if w == nil {
		return ""
	}

	for _, v := range w.ExternalIDs {
		if v.Type == s {
			return v.Value
		}
	}

	return ""
}

// Citation is an ORCID citation field.
type Citation struct {
	Type  string `xml:"citation-type"`
	Value string `xml:"citation-value"`
}

// Contributor is an ORCID contributor.
type Contributor struct {
	Name string `xml:"credit-name"`
}

// WorksModifier is a general type for any function you can pass to FetchWorks
// or ReadWorks to mutate the works with any arbitrary logic which a user of a
// library might need.
type WorksModifier func([]*Work)

// FetchWorks downloads publications from ORCID.
func FetchWorks(c *Client, id ID, logger *log.Logger, mods ...WorksModifier) ([]*Work, error) {
	var works []*Work
	var err error

	logger.Println("downloading via HTTP")
	works, err = fetchWorks(c, id, logger)
	if err != nil {
		return nil, fmt.Errorf("fetchWorks failed: %v", err)
	}

	// sort works in year descending order
	sort.Slice(works, func(i, j int) bool {
		return works[i].Year > works[j].Year
	})

	for _, mod := range mods {
		mod(works)
	}

	return works, nil
}

// ReadWorks decodes publications from an XML-file with publications
// saved as top-level elements. Basically, it decodes an output of
// xml.Marshal([]*Work) back into []*Work.
func ReadWorks(path string, mods ...WorksModifier) ([]*Work, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	d := xml.NewDecoder(f)

	// read top level elements continuously
	works := []*Work{}
	for {
		var work Work
		err = d.Decode(&work)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		works = append(works, &work)
	}

	for _, mod := range mods {
		mod(works)
	}

	return works, nil
}

func fetchWork(uri string) (*Work, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	work := Work{}
	err = xml.NewDecoder(resp.Body).Decode(&work)
	if err != nil {
		return nil, fmt.Errorf("response status: %v, error: %v", resp.Status, err)
	}

	return &work, nil
}

func fetchWorks(c *Client, id ID, logger *log.Logger) ([]*Work, error) {
	// fetch summaries
	relURL, err := url.Parse(fmt.Sprintf("%s/works", id))
	if err != nil {
		return nil, fmt.Errorf("url.Parse failed: %v", err)
	}
	reqURL := c.apiBase.ResolveReference(relURL)
	summaries, err := fetchWorkSummaries(reqURL.String())
	if err != nil {
		return nil, fmt.Errorf("fetchWorkSummaries failed: %v", err)
	}

	// fetch details concurrently

	num := len(*summaries)
	worksCh := make(chan *Work, num)
	var wg sync.WaitGroup
	for n := 0; n < int(math.Ceil(float64(num)/20.0)); n++ {
		start := n * 20
		end := (n + 1) * 20
		if end > num {
			end = num
		}
		for _, w := range (*summaries)[start:end] {
			wg.Add(1)
			go func(w Work, works chan<- *Work) {
				defer wg.Done()

				relURL, err := url.Parse(w.Path)
				if err != nil {
					logger.Printf("url.Parse failed: %v", err)
					return
				}

				reqURL := c.apiBase.ResolveReference(relURL)
				logger.Println("fetching", reqURL.String())
				work, err := fetchWork(reqURL.String())
				if err != nil {
					logger.Println(err)
					return
				}
				works <- work
			}(w, worksCh)
		}
		wg.Wait()
	}
	close(worksCh)

	works := []*Work{}
	for w := range worksCh {
		if w != nil {
			works = append(works, w)
		}
	}

	if len(*summaries) != len(works) {
		logger.Printf("different amount of publications: %v vs %v", len(*summaries), len(works))
	}

	return works, nil
}

func fetchWorkSummaries(uri string) (*[]Work, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return nil, fmt.Errorf("http.Get failed with code %d: %v", resp.StatusCode, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respData, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("http.Get bad response, code %v, request URL: %v, response: %s",
			resp.StatusCode, uri, respData)
	}

	works, err := decodeWorks(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decodeWorks failed: %v", err)
	}

	return works, nil
}

func decodeWorks(src io.Reader) (*[]Work, error) {
	activities := []Activity{}
	err := xml.NewDecoder(src).Decode(&activities)
	if err != nil {
		return nil, err
	}

	works := []Work{}
	for _, a := range activities {
		works = append(works, a.Works...)
	}
	uniqueWorks := filterUniqueTitles(&works)
	return uniqueWorks, nil
}

// filterUniqueTitles filters works by a unique title and adds to the
// returned slice only the first item met. Checking by title, because
// publications with the same titles can have different type of IDs.
func filterUniqueTitles(works *[]Work) *[]Work {
	uniqueWorks := []Work{}
	var unique bool
	for _, w := range *works {
		unique = true
		for _, uw := range uniqueWorks {
			// lowering strings and removing all spaces,
			// because people sometimes mess with spaces
			// and capitalization while naming their works
			if simplifyString(string(w.Title)) == simplifyString(string(uw.Title)) {
				unique = false
				break
			}
		}
		if unique {
			uniqueWorks = append(uniqueWorks, w)
		}
	}
	return &uniqueWorks
}

// simplifyString removes some punctuation from a string.
func simplifyString(s string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(
					strings.ToLower(s),
					" ", ""),
				"(", ""),
			")", ""),
		"-", "")
}
