package orcid

import (
	"bytes"
	"html/template"
	"log"
	"os"
	"reflect"
	"testing"
)

func TestIDFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		res     string
		id      ID
		wantErr bool
	}{
		{
			name:    "A",
			url:     "https://orcid.org/0000-0002-0183-1282",
			res:     "0000-0002-0183-1282",
			id:      ID("0000-0002-0183-1282"),
			wantErr: false,
		},
		{
			name:    "B",
			url:     "orcid.org/0000-0003-1928-5141",
			res:     "0000-0003-1928-5141",
			id:      ID("0000-0003-1928-5141"),
			wantErr: false,
		},
		{
			name:    "C",
			url:     "http://orcid.org//0000-0002-0183-1282",
			res:     "0000-0002-0183-1282",
			id:      ID("0000-0002-0183-1282"),
			wantErr: false,
		},
		{
			name:    "D",
			url:     "orcid.org///0000-0002-0183-1282",
			res:     "0000-0002-0183-1282",
			id:      ID("0000-0002-0183-1282"),
			wantErr: false,
		},
		{
			name:    "E",
			url:     "https://orcid.org/0000-0002-0183-1282/",
			res:     "0000-0002-0183-1282",
			id:      ID("0000-0002-0183-1282"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := IDFromURL(tt.url)
			if err != nil && !tt.wantErr {
				t.Error(err)
			}
			if tt.res != id.String() {
				t.Errorf("ids should match, want %v, got %v", tt.res, id.String())
			}
			if !reflect.DeepEqual(tt.id, id) {
				t.Errorf("ids should be equal, want %+v, got %+v", tt.id, id)
			}
		})
	}
}

func TestFetchWorks(t *testing.T) {
	args := [][]string{
		[]string{"https://orcid.org/0000-0003-1928-5141", "0000-0003-1928-5141"},
		[]string{"orcid.org/0000-0002-0183-1282", "0000-0002-0183-1282"},
	}

	const apiBase = "https://pub.orcid.org/v2.1"
	var logger = log.New(os.Stdout, "", log.LstdFlags)

	client, err := New(apiBase)
	if err != nil {
		t.Error(err)
	}

	for _, arg := range args {
		id, err := IDFromURL(arg[0])
		if err != nil {
			t.Error(err)
		}

		if idStr := id.String(); idStr != arg[1] {
			t.Errorf("want %v, got %v", arg[1], idStr)
		}

		works, err := FetchWorks(client, id, logger, UpdateExternalIDsURL, UpdateContributorsLine, UpdateMarkup)
		if err != nil {
			t.Error(err)
		}
		if len(works) == 0 {
			t.Error("amount of works must be bigger than zero")
		}
	}
}

func TestRender(t *testing.T) {
	paths := []string{
		"testdata/works.xml",
	}

	f, err := os.Open(paths[0])
	if err != nil {
		t.Error(err)
	}
	defer f.Close()

	works, err := decodeWorks(f)
	if err != nil {
		t.Error(err)
	}
	if works == nil {
		t.Error("works must not be nil")
	}

	worksIntf := make([]interface{}, len(*works))
	for i := range *works {
		worksIntf[i] = (*works)[i]
	}

	const (
		tmplPath     = "testdata/pubs.html"
		contentModel = "wikitext"
	)

	renderTmpl := func(data interface{}, tmplPath string) (string, error) {
		var tmpl = template.Must(template.ParseFiles(tmplPath))
		var out bytes.Buffer
		err := tmpl.Execute(&out, data)
		return out.String(), err
	}

	markup, err := renderTmpl(works, tmplPath)
	//t.Logf("output: %v", markup)
	if err != nil {
		t.Error(err)
	}
	if len(markup) == 0 {
		t.Error("length of markup must be greater than zero")
	}
}
