package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"bitbucket.org/iharsuvorau/crossref"
	"bitbucket.org/iharsuvorau/mediawiki"
	"bitbucket.org/iharsuvorau/orcid"
	"github.com/pkg/errors"
)

func main() {
	uri := flag.String("mwuri", "localhost/mediawiki", "mediawiki URI")
	crossrefApiURL := flag.String("crossref", "http://api.crossref.org/v1", "crossref API base URL")
	section := flag.String("section", "Publications", "section title for the publication to look for on a user's page or of the new one to add to the page")
	category := flag.String("category", "", "category of users to update profile pages for, if it's empty all users' pages will be updated")
	name := flag.String("name", "", "login name of the bot for updating pages")
	pass := flag.String("pass", "", "login password of the bot for updating pages")
	logPath := flag.String("log", "", "specify the filepath for a log file, if it's empty all messages are logged into stdout")
	flag.Parse()
	if len(*uri) == 0 || len(*name) == 0 || len(*pass) == 0 || len(*section) == 0 {
		log.Fatal("all flags are compulsory, use -h to see the documentation")
	}

	// Log everything to os.Stdout, a file or discard with ioutil.Discard.
	var logger *log.Logger
	if len(*logPath) > 0 {
		f, err := os.Create(*logPath)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		logger = log.New(f, "", log.LstdFlags)
	} else {
		logger = log.New(os.Stdout, "", log.LstdFlags)
	}

	// Update publications on personal pages for each user.

	// exploreUsers discovers users which profiles should be updated by the
	// category assigned to their profile pages. If you know all your users
	// you don't have to discover them, create a slice of users with their
	// names and registries by hand.
	users, err := exploreUsers(*uri, *category, logger)
	if err != nil {
		log.Fatal(err)
	}
	logger.Printf("users to update: %+v", len(users))

	cref, err := crossref.New(*crossrefApiURL)
	if err != nil {
		log.Fatal(err)
	}

	// updateProfilePages overwrites the section on a user's profile page
	// with the downloaded publications from registries.
	err = updateProfilePages(*uri, *name, *pass, *section, users, logger, cref)
	if err != nil {
		log.Fatal(err)
	}

	// Publications by year

	// To create an aggregate page of PI publications sorted by year, first
	// we need PI users.
	usersPI, err := exploreUsers(*uri, "PI", logger)
	if err != nil {
		log.Fatal(err)
	}
	logger.Printf("PI users to process: %+v", len(usersPI))

	if err = updatePublicationsByYear(*uri, *name, *pass, usersPI, logger); err != nil {
		log.Fatal(err)
	}
}

// User is a MediaWiki user with registries which handle publications.
type User struct {
	Title string
	Orcid *orcid.Registry
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

// updateProfilePages fetches works for each user, updates personal pages and
// purges cache for the aggregate Publications page.
func updateProfilePages(mwURI, loginName, loginPass, sectionTitle string, users []*User, logger *log.Logger, cref *crossref.Client) error {
	if users == nil {
		return nil
	}

	const (
		tmpl         = "orcid-list.tmpl"
		contentModel = "wikitext"
	)

	for _, u := range users {
		logger.Printf("fetching works for %v", u.Title)

		// TODO: use goroutines
		works, err := u.Orcid.FetchWorks(logger)
		if err != nil {
			return err
		}

		err = getMissingAuthorsCrossRef(cref, works, logger)
		if err != nil {
			return err
		}

		byTypeAndYear := groupByTypeAndYear(works)

		logger.Println("publications has been downloaded")
		markup, err := renderTmpl(byTypeAndYear, tmpl)
		if err != nil {
			return err
		}

		logger.Println("publications has been rendered")
		_, err = mediawiki.UpdatePage(mwURI, u.Title, markup, contentModel,
			loginName, loginPass, sectionTitle)
		if err != nil {
			return err
		}
		logger.Printf("profile page for %s is updated", u.Title)
	}
	return mediawiki.Purge(mwURI, "Publications")
}

func getMissingAuthorsCrossRef(cref *crossref.Client, works []*orcid.Work, logger *log.Logger) error {
	for _, w := range works {
		// skip if there are authors already
		if len(w.Contributors) > 0 {
			continue
		}

		// DOI check
		if len(string(w.DoiURI)) == 0 {
			logger.Printf("publication doesn't have DOI: %v", w.Title)
			continue
		}

		// crossref download
		id, err := crossref.DOIFromURL(string(w.DoiURI))
		if err != nil {
			return err
		}
		work, err := crossref.GetWork(cref, id)
		if err != nil {
			return err
		}
		type contrib struct {
			Name string `xml:"credit-name"`
		}
		for _, v := range work.Authors {
			w.Contributors = append(w.Contributors, contrib{Name: v})
		}
	}

	return nil
}

func updatePublicationsByYear(mwURI, loginName, loginPass string, users []*User, logger *log.Logger) error {
	if users == nil {
		return nil
	}

	const (
		tmpl         = "publications-by-year.tmpl"
		pageTitle    = "PI_Publications_By_Year"
		contentModel = "wikitext"
		sectionTitle = "Publications By Year"
	)

	logger.Printf("updating %s...", pageTitle)

	// fetching works for all users
	var works = []*orcid.Work{}
	for _, u := range users {
		logger.Printf("fetching works for %v", u.Title)
		// TODO: use goroutines

		// fetch only if no XML is saved

		var fpath = u.Orcid.UserID() + ".xml"
		var w = []*orcid.Work{}
		var err error

		if orcid.IsFileNew(fpath, time.Hour*24) {
			w, err = orcid.ReadWorks(fpath)
			if err != nil {
				return err
			}
		} else {
			w, err = u.Orcid.FetchWorks(logger)
			if err != nil {
				return err
			}
		}

		works = append(works, w...)
	}

	byTypeAndYear := groupByTypeAndYear(works)

	// updating the page
	markup, err := renderTmpl(byTypeAndYear, tmpl)
	if err != nil {
		return err
	}
	_, err = mediawiki.UpdatePage(mwURI, pageTitle, markup, contentModel,
		loginName, loginPass, sectionTitle)
	if err != nil {
		return err
	}

	// Purging of neccessary pages.
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
