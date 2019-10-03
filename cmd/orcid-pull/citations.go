package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"bitbucket.org/iharsuvorau/orcid"
	"github.com/nickng/bibtex"
)

func applyRegexp(re *regexp.Regexp, s string) ([]string, error) {
	matches := re.FindStringSubmatch(s)
	if len(matches) == 0 {
		return []string{}, fmt.Errorf("no matches for %s", s)
	}

	return matches, nil
}

func parseCitationAuthorsIEEE(s string) (string, error) {
	// TODO: many different formats

	// test case C, in case of correct BibTeX format
	if strings.HasPrefix(strings.Trim(s, " "), "@") {
		return parseCitationAuthorsBibTeXStrict(s)
	}

	// other cases

	// test case A
	re := regexp.MustCompile(`.*\(\d{4}\)`)
	matches, err := applyRegexp(re, s)
	if err == nil {
		// only the first match matters, because authors should be in the
		// beginning of the string
		match := matches[0]

		// cleaning up
		match = match[:len(match)-7] // remove (YYYY) at the end
		match = strings.Trim(match, "\"")
		match = strings.Trim(match, " ")
		match = strings.Trim(match, ".")
		match = strings.Trim(match, ",")

		return match, nil
	}

	// test case B
	re = regexp.MustCompile(`.*[\W|,|.|\s]{2,}"`)
	matches, err = applyRegexp(re, s)
	if err == nil {
		// only the first match matters, because authors should be in the
		// beginning of the string
		match := matches[0]

		// cleaning up
		match = strings.Trim(match, "\"")
		match = strings.Trim(match, " ")
		match = strings.Trim(match, ".")
		match = strings.Trim(match, ",")
		return match, nil
	}

	return "", err
}

func parseCitationAuthorsBibTeX(s string) (string, error) {
	// test case B, in case of correct BibTeX format
	if strings.HasPrefix(strings.Trim(s, " "), "@") {
		return parseCitationAuthorsBibTeXStrict(s)
	}

	// other cases

	re := regexp.MustCompile(`(.*)?(?:\W\s")`)
	matches, err := applyRegexp(re, s)

	if err != nil {
		return "", err
	}

	// only the first match matters, because authors should be in the
	// beginning of the string
	match := matches[0]

	// cleaning up
	match = strings.Trim(match, "\"")
	match = strings.Trim(match, " ")
	match = strings.Trim(match, ".")
	match = strings.Trim(match, ",")

	return match, nil
}

func parseCitationAuthorsBibTeXStrict(s string) (string, error) {
	r := strings.NewReader(s)
	bib, err := bibtex.Parse(r)
	if err != nil {
		return "", err
	}
	var authors string
	for _, v := range bib.Entries {
		authors = v.Fields["author"].String()
	}
	return authors, nil
}

func citationContributors(w *orcid.Work, logger *log.Logger) []*orcid.Contributor {
	if len(w.Contributors) > 0 {
		return nil
	}

	if w.Citation == nil {
		return nil
	}

	var err error
	var authors string
	var contribs = []*orcid.Contributor{}

	switch w.Citation.Type {
	case "formatted-ieee":
		authors, err = parseCitationAuthorsIEEE(w.Citation.Value)
		break
	case "bibtex":
		authors, err = parseCitationAuthorsBibTeX(w.Citation.Value)
		break
	default:
		logger.Printf("unsupported citation type: %s", w.Citation.Type)
		return nil
	}

	if err != nil {
		logger.Println(err)
		return nil
	}

	// adding all authors as one, because it's not always clear how it's
	// better to separate authors
	contribs = append(contribs, &orcid.Contributor{Name: authors})

	return contribs
}
