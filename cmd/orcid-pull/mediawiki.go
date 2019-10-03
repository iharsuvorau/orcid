package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"sort"
	"strings"
	"sync"

	"bitbucket.org/iharsuvorau/crossref"
	"bitbucket.org/iharsuvorau/mediawiki"
	"bitbucket.org/iharsuvorau/orcid"
	"github.com/pkg/errors"
)

// User is a MediaWiki user with registries which handle publications.
type User struct {
	Title string
	Orcid *orcid.Registry
	Works []*orcid.Work
}

// exploreUsers gets users who belong to the category and fetches their
// publication IDs and creates corresponding registries. If the category is
// empty, all users are returned.
func exploreUsers(mwURI, category string, logger *log.Logger) ([]*User, error) {
	var userTitles []string
	var err error

	if len(category) > 0 {
		userTitles, err = mediawiki.GetCategoryMembers(mwURI, category)
	} else {
		var userNames []string
		userNames, err = mediawiki.GetUsers(mwURI)
		// formatting a username into a page title
		userTitles = make([]string, len(userNames))
		for i := range userNames {
			userTitles[i] = fmt.Sprintf("User:%s", strings.ReplaceAll(userNames[i], " ", "_"))
		}
	}
	if err != nil {
		return nil, err
	}

	users := []*User{}
	var mut sync.Mutex
	var limit = 20
	sem := make(chan bool, limit)
	errs := make(chan error)

	for _, title := range userTitles {
		sem <- true
		go func(title string) {
			defer func() { <-sem }()

			// fetch each user external links from the profile page
			links, err := mediawiki.GetExternalLinks(mwURI, title)
			if err != nil {
				// TODO: unify the usage of errors or delete it
				errs <- errors.Wrapf(err, "GetExternalLinks failed with user title: %s", title)
			}
			if len(links) == 0 {
				return
			}

			// means there are any external links on a profile page
			logger.Printf("%v discovered", title)

			// create a user and registries
			user := User{Title: title}
			for _, link := range links {
				// adding the ORCID registry
				if strings.Contains(link, "orcid.org") {
					r, err := orcid.New(link)
					if err != nil {
						errs <- errors.Wrap(err, "failed to create orcid registry")
					}
					user.Orcid = r
					// break after the first appending to
					// add only one orcid from each user
					// page
					break
				}
			}

			// return only users for whom we need to update profile pages
			if user.Orcid != nil {
				mut.Lock()
				users = append(users, &user)
				mut.Unlock()
			}
		}(title)
	}
	for i := 0; i < cap(sem); i++ {
		sem <- true
	}
	close(errs)
	close(sem)
	for err := range errs {
		if err != nil {
			return nil, errors.Wrap(err, "failed to collect an offer")
		}
	}

	return users, err
}

// updateProfilePagesWithWorks fetches works for each user, updates personal pages and
// purges cache for the aggregate Publications page.
func updateProfilePagesWithWorks(mwURI, lgName, lgPass, sectionTitle string, users []*User, logger *log.Logger, cref *crossref.Client) error {
	if len(users) == 0 {
		return nil
	}

	const (
		tmpl         = "orcid-list.tmpl" // TODO: should be passed by a user
		contentModel = "wikitext"
	)

	for _, u := range users {
		byTypeAndYear := groupByTypeAndYear(u.Works)

		markup, err := renderTmpl(byTypeAndYear, tmpl)
		if err != nil {
			return err
		}

		_, err = mediawiki.UpdatePage(mwURI, u.Title, markup, contentModel, lgName, lgPass, sectionTitle)
		if err != nil {
			return err
		}

		logger.Printf("profile page for %s is updated", u.Title)
	}

	return nil
}

func updatePublicationsByYearWithWorks(mwURI, lgName, lgPass string, users []*User, logger *log.Logger, cref *crossref.Client) error {
	if len(users) == 0 {
		return nil
	}

	const (
		tmpl         = "publications-by-year.tmpl" // TODO: should be passed by a user
		pageTitle    = "PI_Publications_By_Year"
		contentModel = "wikitext"
		sectionTitle = "Publications By Year"
	)

	var works = []*orcid.Work{}
	for _, u := range users {
		works = append(works, u.Works...)
	}

	byTypeAndYear := groupByTypeAndYear(works)

	markup, err := renderTmpl(byTypeAndYear, tmpl)
	if err != nil {
		return err
	}
	_, err = mediawiki.UpdatePage(mwURI, pageTitle, markup, contentModel, lgName, lgPass, sectionTitle)
	if err != nil {
		return err
	}

	logger.Printf("%s page has been updated", pageTitle)
	return mediawiki.Purge(mwURI, "Publications")
}

func renderTmpl(data interface{}, tmplPath string) (string, error) {
	var tmpl = template.Must(template.ParseFiles(tmplPath))
	var out bytes.Buffer
	err := tmpl.Execute(&out, data)
	return out.String(), err
}

func getYearsSorted(works []*orcid.Work) []int {
	var years = make(map[int]bool)
	for _, w := range works {
		years[w.Year] = true
	}

	var yearsSorted = []int{}
	for k := range years {
		yearsSorted = append(yearsSorted, k)
	}

	sort.Slice(yearsSorted, func(i, j int) bool {
		return yearsSorted[i] > yearsSorted[j]
	})

	return yearsSorted
}

func groupByTypeAndYear(works []*orcid.Work) map[string][][]*orcid.Work {
	// grouping by work type
	byType := make(map[string][]*orcid.Work)
	const (
		t1 = "Journal Articles"
		t2 = "Conference Papers"
		t3 = "Other"
	)
	byType[t1] = []*orcid.Work{}
	byType[t2] = []*orcid.Work{}
	byType[t3] = []*orcid.Work{}
	for _, w := range works {
		switch w.Type {
		case "journal-article":
			byType[t1] = append(byType[t1], w)
			continue
		case "conference-paper":
			byType[t2] = append(byType[t2], w)
			continue
		default:
			byType[t3] = append(byType[t3], w)
		}
	}

	// grouping each type group by year
	byTypeAndYear := make(map[string][][]*orcid.Work)
	for k, group := range byType {
		years := getYearsSorted(group)

		if byTypeAndYear[k] == nil {
			byTypeAndYear[k] = make([][]*orcid.Work, len(years))
		}

		for i, year := range years {
			if byTypeAndYear[k][i] == nil {
				byTypeAndYear[k][i] = []*orcid.Work{}
			}

			for _, w := range group {
				if w.Year == year {
					byTypeAndYear[k][i] = append(byTypeAndYear[k][i], w)
				}
			}
		}
	}

	return byTypeAndYear
}
