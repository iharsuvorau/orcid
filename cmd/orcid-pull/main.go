package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"bitbucket.org/iharsuvorau/crossref"
	"bitbucket.org/iharsuvorau/orcid"
)

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

	// saving XML for each user
	{
		var fpath string
		var err error
		var obsoleteDuration = time.Hour * 23 // TODO: time condition should be passed by a caller
		for _, u := range users {
			fpath = u.Orcid.UserID() + ".xml"
			if !isFileNew(fpath, obsoleteDuration) { // saves once in 23 hours
				err = dumpUserWorks(u)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
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
			u.Works, err = u.Orcid.FetchWorks(logger)
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

			w.Contributors = crossRefContributors(w, cref, logger)

			// skip if there are authors already
			if len(w.Contributors) > 0 {
				continue
			}

			w.Contributors = citationContributors(w, logger)
		}
	}

	return nil
}
