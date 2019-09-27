package main

import (
	"log"
	"os"
	"testing"

	"bitbucket.org/iharsuvorau/crossref"
	"bitbucket.org/iharsuvorau/orcid"
)

func TestExploreUsers(t *testing.T) {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	args := []struct {
		name      string
		uri       string
		category  string
		wantErr   bool
		zeroUsers bool
	}{
		{
			name:      "A",
			uri:       "http://hefty.local/~ihar/ims/1.32.2",
			category:  "PI",
			wantErr:   false,
			zeroUsers: false,
		},
		{
			name:      "B",
			uri:       "http://hefty.local/~ihar/ims/1.32.2/",
			category:  "PI",
			wantErr:   false,
			zeroUsers: false,
		},
		{
			name:      "C",
			uri:       "http://hefty.local/~ihar/ims/1.32.2/",
			category:  "",
			wantErr:   false,
			zeroUsers: false,
		},
		{
			name:      "D",
			uri:       "http://hefty.local/",
			category:  "",
			wantErr:   true,
			zeroUsers: true,
		},
		{
			name:      "E",
			uri:       "http://hefty.local/",
			category:  "PI",
			wantErr:   true,
			zeroUsers: true,
		},
	}

	for _, arg := range args {
		t.Run(arg.name, func(t *testing.T) {
			users, err := exploreUsers(arg.uri, arg.category, logger)
			if users != nil {
				t.Logf("users len: %v", len(users))
			}
			if err != nil && !arg.wantErr {
				t.Error(err)
			}
			if users != nil && len(users) == 0 && !arg.zeroUsers {
				t.Errorf("amount of users must be gt 0, arg: %+v", arg)
			}

		})
	}
}

func Test_groupByTypeAndYear(t *testing.T) {
	ids := []string{
		"https://orcid.org/0000-0002-1720-1509",
		"https://orcid.org/0000-0002-9151-1548",
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	for _, id := range ids {
		registry, err := orcid.New(id)
		if err != nil {
			t.Fatal(err)
		}

		works, err := registry.FetchWorks(logger)
		if err != nil {
			t.Fatal(err)
		}

		if len(works) == 0 {
			t.Error("amount of works must be bigger than zero")
		}

		byTypeAndYear := groupByTypeAndYear(works)
		t.Logf("result: %+v", byTypeAndYear)

		markup, err := renderTmpl(byTypeAndYear, "publications-by-year.tmpl")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("markup: %s", markup)
		//t.Fail()
	}
}

func Test_getMissingAuthorsCrossRef(t *testing.T) {
	ids := []string{
		"https://orcid.org/0000-0001-8221-9820",
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	cref, err := crossref.New("http://api.crossref.org/v1")
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range ids {
		registry, err := orcid.New(id)
		if err != nil {
			t.Fatal(err)
		}

		works, err := registry.FetchWorks(logger)
		if err != nil {
			t.Fatal(err)
		}

		if len(works) == 0 {
			t.Error("amount of works must be bigger than zero")
		}

		t.Logf("contributors before: %+v", works[0].Contributors)

		err = getMissingAuthorsCrossRef(cref, works[:1], logger)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("contributors after: %+v", works[0].Contributors)
	}
}

func Test_fetchPublicationsAndMissingAuthors(t *testing.T) {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	const mwBaseURL = "http://hefty.local/~ihar/ims/1.32.2"
	const category = "PI"
	const crossrefApiURL = "http://api.crossref.org/v1"

	users, err := exploreUsers(mwBaseURL, category, logger)
	if err != nil {
		t.Error(err)
	}

	err = fetchPublications(logger, users)
	if err != nil {
		t.Fatal(err)
	}

	for _, u := range users {
		if l := len(u.Works); l == 0 {
			t.Errorf("want more works, have %v", l)
		}
	}

	// limit the number of users and works
	if len(users) > 1 {
		users = users[:1]
	}
	if len(users[0].Works) > 2 {
		users[0].Works = users[0].Works[:2]
	}

	t.Log("before")
	for _, u := range users {
		t.Log(u.Title)
		for _, w := range u.Works {
			t.Log(w.Title)
			t.Logf("authors: %+v", w.Contributors)
		}
	}

	cref, err := crossref.New(crossrefApiURL)
	if err != nil {
		log.Fatal(err)
	}

	err = fetchMissingAuthors(cref, logger, users)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("after")
	for _, u := range users {
		t.Log(u.Title)
		for _, w := range u.Works {
			t.Log(w.Title)
			t.Logf("authors: %+v", w.Contributors)
		}
	}
}
