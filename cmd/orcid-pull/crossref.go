package main

import (
	"log"
	"time"

	"bitbucket.org/iharsuvorau/crossref"
	"bitbucket.org/iharsuvorau/orcid"
)

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

func crossRefContributors(w *orcid.Work, cref *crossref.Client, logger *log.Logger) []*orcid.Contributor {
	if len(w.Contributors) > 0 {
		return nil
	}

	// DOI check
	if len(string(w.DoiURI)) == 0 {
		logger.Printf("publication doesn't have DOI: %v", w.Title)
		return nil
	}

	// crossref download
	id, err := crossref.DOIFromURL(string(w.DoiURI))
	if err != nil {
		log.Println(err)
		return nil
	}

	logger.Printf("crossref fetch: %s, %s", w.Title, id)
	work, err := crossref.GetWork(cref, id)
	if err != nil {
		logger.Printf("crossref fetch error: %v", err)
		time.Sleep(time.Second * 1) // give the server time to rest
		return nil
	}

	contribs := []*orcid.Contributor{}
	for _, v := range work.Authors {
		contribs = append(contribs, &orcid.Contributor{Name: v})
	}

	return contribs
}
