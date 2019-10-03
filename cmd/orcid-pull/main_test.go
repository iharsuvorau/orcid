package main

import (
	"log"
	"os"
	"reflect"
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
	// if len(users[0].Works) > 2 {
	// 	users[0].Works = users[0].Works[:2]
	// }

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
			for _, v := range w.Contributors {
				t.Logf("\tauthor: %+v", v)
			}
		}
	}
}

func Test_parseCitationAuthorsIEEE(t *testing.T) {
	tests := []struct {
		name     string
		citation string
		result   string
		wantErr  bool
	}{
		{
			name:     "A",
			citation: "Saoni Banerji, R.Senthil Kumar (2010). Diagnosis of Systems Via Condition Monitoring Based on Time Frequency Representations. International Journal of Recent Trends in Engineering & Research, 4 (2), 20−24.01.IJRTET 04.02.102.",
			result:   "Saoni Banerji, R.Senthil Kumar",
			wantErr:  false,
		},
		{
			name:     "B",
			citation: `S. Banerji, J. Madrenas and D. Fernandez, "Optimization of parameters for CMOS MEMS resonant pressure sensors," 2015 Symposium on Design, Test, Integration and Packaging of MEMS/MOEMS (DTIP), Montpellier, 2015, pp. 1-6. doi: 10.1109/DTIP.2015.7160984`,
			result:   "S. Banerji, J. Madrenas and D. Fernandez",
			wantErr:  false,
		},
		{
			name:     "C",
			citation: ` @phdthesis{banerji2012ultrasonic, title= {Ultrasonic Link IC for Wireless Power and Data Transfer Deep in Body}, author= {Banerji, Saoni and Ling, Goh Wang and Cheong, Jia Hao and Je, Minkyu}, year= {2012}, school= {Nanyang Technological University}} `,
			result:   "Banerji, Saoni and Ling, Goh Wang and Cheong, Jia Hao and Je, Minkyu",
			wantErr:  false,
		},
		{
			name:     "D",
			citation: `Banerji, Saoni & Chiva, Josep. (2016). Under pressure? Do not lose direction! Smart sensors: Development of MEMS and CMOS on the same platform.`,
			result:   "Banerji, Saoni & Chiva, Josep",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseCitationAuthorsIEEE(tt.citation)
			if err != nil && !tt.wantErr {
				t.Error(err)
			}
			if !reflect.DeepEqual(res, tt.result) {
				t.Errorf("want %q, got %q", tt.result, res)
			}
		})
	}
}

func Test_parseCitationAuthorsBibTeX(t *testing.T) {
	tests := []struct {
		name     string
		citation string
		result   string
		wantErr  bool
	}{
		{
			name: "A",
			citation: `S. Banerji, W. L. Goh, J. H. Cheong and M. Je, "CMUT ultrasonic power link front-end for wireless power transfer deep in body," 2013 IEEE MTT-S International Microwave Workshop Series on RF and Wireless Technologies for Biomedical and Healthcare Applications (IMWS-BIO), Singapore, 2013, pp. 1-3.
doi: 10.1109/IMWS-BIO.2013.6756176`,
			result:  "S. Banerji, W. L. Goh, J. H. Cheong and M. Je",
			wantErr: false,
		},
		{
			name:     "B",
			citation: `@inproceedings{Vunder_2018,doi = {10.1109/hsi.2018.8431062},url = {https://doi.org/10.1109%2Fhsi.2018.8431062},year = 2018,month = {jul},publisher = {{IEEE}},author = {Veiko Vunder and Robert Valner and Conor McMahon and Karl Kruusamae and Mitch Pryor},title = {Improved Situational Awareness in {ROS} Using Panospheric Vision and Virtual Reality},booktitle = {2018 11th International Conference on Human System Interaction ({HSI})}}`,
			result:   "Veiko Vunder and Robert Valner and Conor McMahon and Karl Kruusamae and Mitch Pryor",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseCitationAuthorsBibTeX(tt.citation)
			if err != nil && !tt.wantErr {
				t.Error(err)
			}
			if !reflect.DeepEqual(res, tt.result) {
				t.Errorf("want %q, got %q", tt.result, res)
			}
		})
	}
}

func Test_parseCitationAuthorsBibTeXStrict(t *testing.T) {
	tests := []struct {
		name     string
		citation string
		result   string
		wantErr  bool
	}{
		{
			name: "A",
			citation: `S. Banerji, W. L. Goh, J. H. Cheong and M. Je, "CMUT ultrasonic power link front-end for wireless power transfer deep in body," 2013 IEEE MTT-S International Microwave Workshop Series on RF and Wireless Technologies for Biomedical and Healthcare Applications (IMWS-BIO), Singapore, 2013, pp. 1-3.
doi: 10.1109/IMWS-BIO.2013.6756176`,
			result:  "",
			wantErr: true,
		},
		{
			name:     "B",
			citation: `@inproceedings{Vunder_2018,doi = {10.1109/hsi.2018.8431062},url = {https://doi.org/10.1109%2Fhsi.2018.8431062},year = 2018,month = {jul},publisher = {{IEEE}},author = {Veiko Vunder and Robert Valner and Conor McMahon and Karl Kruusamae and Mitch Pryor},title = {Improved Situational Awareness in {ROS} Using Panospheric Vision and Virtual Reality},booktitle = {2018 11th International Conference on Human System Interaction ({HSI})}}`,
			result:   "Veiko Vunder and Robert Valner and Conor McMahon and Karl Kruusamae and Mitch Pryor",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseCitationAuthorsBibTeXStrict(tt.citation)
			if err != nil && !tt.wantErr {
				t.Error("parsing failed:", err)
			}
			if !reflect.DeepEqual(res, tt.result) {
				t.Errorf("want %q, got %q", tt.result, res)
			}
		})
	}
}
