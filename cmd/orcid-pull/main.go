package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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

func main() {
	mwBaseURL := flag.String("mediawiki", "https://ims.ut.ee", "mediawiki base URL")
	crossrefApiURL := flag.String("crossref", "http://api.crossref.org/v1", "crossref API base URL")
	section := flag.String("section", "Publications", "section title for the publication to look for on a user's page or of the new one to add to the page")
	category := flag.String("category", "", "category of users to update profile pages for, if it's empty all users' pages will be updated")
	lgName := flag.String("name", "", "login name of the bot for updating pages")
	lgPass := flag.String("pass", "", "login password of the bot for updating pages")
	logPath := flag.String("log", "", "specify the filepath for a log file, if it's empty all messages are logged into stdout")
	flag.Parse()

	flagsStringFatalCheck(mwBaseURL, crossrefApiURL, section, lgName, lgPass)

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

	cref, err := crossref.New(*crossrefApiURL)
	if err != nil {
		logger.Fatal(err)
	}

	//
	// Publications for each user
	//

	users, err := exploreUsers(*mwBaseURL, *category, logger)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("users to update: %+v", len(users))

	// TODO: it's not clear, that orcid.FetchWorks is going to look at a
	// specific file in the file system first for publications and that
	// it's going to save XML also
	if err = fetchPublications(logger, users); err != nil {
		logger.Fatal(err)
	}

	err = fetchMissingAuthors(cref, logger, users)
	if err != nil {
		logger.Fatal(err)
	}

	// used by the template in updateProfilePagesWithWorks
	updateContributorsLine(users) // TODO: make cleaner, hide this detail

	err = updateProfilePagesWithWorks(*mwBaseURL, *lgName, *lgPass, *section, users, logger, cref)
	if err != nil {
		logger.Fatal(err)
	}

	//
	// Publications on the Publications page
	//

	// TODO: repetitive section of code

	usersPI, err := exploreUsers(*mwBaseURL, "PI", logger)
	if err != nil {
		log.Fatal(err)
	}
	logger.Printf("PI users to process: %+v", len(usersPI))

	if err = fetchPublications(logger, usersPI); err != nil {
		logger.Fatal(err)
	}

	err = fetchMissingAuthors(cref, logger, usersPI)
	if err != nil {
		logger.Fatal(err)
	}

	// used by the template in updateProfilePagesWithWorks
	updateContributorsLine(usersPI) // TODO: make cleaner, hide this detail

	err = updatePublicationsByYearWithWorks(*mwBaseURL, *lgName, *lgPass, usersPI, logger, cref)
	if err != nil {
		logger.Fatal(err)
	}
}

func flagsStringFatalCheck(ss ...*string) {
	for _, s := range ss {
		if len(*s) == 0 {
			log.Fatalf("fatal: flag %s has the length of zero", *s)
		}
	}
}

func dumpUserWorks(u *User) error {
	fpath := u.Orcid.UserID() + ".xml"
	f, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", fpath, err)
	}
	if err = xml.NewEncoder(f).Encode(u.Works); err != nil {
		return fmt.Errorf("failed to encode xml: %+v", err)
	}
	f.Close()
	return nil
}

// isFileNew checks the existence of a file and its modification time
// and returns true if it was modified during the previous maxDuration
// hours.
func isFileNew(fpath string, maxDuration time.Duration) bool {
	f, err := os.Open(fpath)
	defer func() {
		err = f.Close()
		if err != nil {
			log.Println("isFileNew error:", err)
		}
	}()

	if err != nil {
		return false
	}

	stat, err := os.Stat(fpath)
	if err != nil {
		return false
	}

	return time.Since(stat.ModTime()) < maxDuration
}

// updateContributorsLine populates a slice of works with an URI for
// external ids if its value is missing.
func updateContributorsLine(users []*User) {
	for _, u := range users {
		for _, w := range u.Works {
			contribs := make([]string, len(w.Contributors))
			for ii, c := range w.Contributors {
				contribs[ii] = c.Name
			}

			// formatting of contributors is according to
			// https://research.moreheadstate.edu/c.php?g=107001&p=695197
			w.ContributorsLine = strings.Join(contribs, ", ")
		}

	}

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

func fetchPublications(logger *log.Logger, users []*User) error {
	if len(users) == 0 {
		return nil
	}

	var err error
	var fpath string
	// files become obsolete 1 hour before the next cron run
	var obsoleteDuration = time.Hour * 23 // TODO: time condition should be passed by a caller

	for _, u := range users { // TODO: use goroutines
		fpath = u.Orcid.UserID() + ".xml"
		if isFileNew(fpath, obsoleteDuration) {
			logger.Printf("reading from a file for %v", u.Title)
			u.Works, err = orcid.ReadWorks(fpath)
		} else {
			logger.Printf("fetching works from ORCID for %v", u.Title)
			if u.Works, err = u.Orcid.FetchWorks(logger); err != nil {
				return err
			}
			err = dumpUserWorks(u)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func fetchMissingAuthors(cref *crossref.Client, logger *log.Logger, users []*User) error {
	logger.Println("starting crossref authors checking")
	start := time.Now()
	defer func() {
		logger.Println("crossref authors checking has been finished in", time.Since(start))
	}()

	for _, u := range users {
		for _, w := range u.Works {
			// skip if there are authors already
			if len(w.Contributors) > 0 {
				continue
			}

			// DOI check
			if len(string(w.DoiURI)) == 0 {
				//logger.Printf("publication doesn't have DOI: %v", w.Title)
				continue
			}

			// crossref download
			id, err := crossref.DOIFromURL(string(w.DoiURI))
			if err != nil {
				return err
			}
			logger.Printf("crossref fetch: %s, %s", w.Title, id)
			work, err := crossref.GetWork(cref, id)
			if err != nil {
				logger.Printf("crossref fetch error: %v", err)
				err = nil                   // ignore this error
				time.Sleep(time.Second * 1) // let the CrossServer to rest a bit
				continue
			}

			if w.Contributors == nil {
				w.Contributors = []*orcid.Contributor{}
			}

			for _, v := range work.Authors {
				w.Contributors = append(w.Contributors, &orcid.Contributor{Name: v})
			}
		}
	}

	return nil
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
		logger.Printf("crossref fetch: %s, %s", w.Title, id)
		work, err := crossref.GetWork(cref, id)
		if err != nil {
			logger.Printf("crossref fetch error: %v", err)
			time.Sleep(time.Second * 1)
			continue
			//return err
		}

		for _, v := range work.Authors {
			w.Contributors = append(w.Contributors, &orcid.Contributor{Name: v})
		}
	}

	return nil
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

func parseCitationAuthorsIEEE(s string) []string {
	// TODO: many different formats and name dividers

	re := regexp.MustCompile(`.*\(\d{4}\)`)
	matches := re.FindStringSubmatch(s)

	if len(matches) == 0 {
		return nil
	}

	match := matches[0]
	match = match[:len(match)-7] // remove (YYYY) at the end
	match = strings.Trim(match, "\"")
	match = strings.Trim(match, " ")
	match = strings.Trim(match, ".")
	authors := strings.Split(match, ", ")
	return authors
}

func parseCitationAuthorsBibTeX(s string) []string {
	// TODO: check for @ at the beginning to parse machine readable info: author={... and ... and ...}

	re := regexp.MustCompile(`(.*)?(?:\W\s")`)
	matches := re.FindStringSubmatch(s)

	if len(matches) == 0 {
		return nil
	}

	match := matches[0]
	match = strings.Trim(match, "\"")
	match = strings.Trim(match, " ")
	match = strings.Trim(match, ",")
	authors := strings.Split(match, ", ")
	return authors
}
