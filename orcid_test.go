package orcid

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"testing"
)

// func Test_markup(t *testing.T) {
// 	args := [][]string{
// 		{
// 			"Libuda, J. ZnO Nanoparticle Formation from the Molecular Precursor [MeZnOtBu<inf>4</inf>by Ozone Treatment",
// 			"Libuda, J. ZnO Nanoparticle Formation from the Molecular Precursor [MeZnOtBu<sub>4</sub>by Ozone Treatment",
// 		},
// 		{
// 			"Molecular Precursor [MeZnOtBu]&lt;inf&gt;4&lt;/inf&gt;by Ozone Treatment",
// 			"Molecular Precursor [MeZnOtBu]<sub>4</sub>by Ozone Treatment",
// 		},
// 	}

// 	works := make([]*Work, len(args))
// 	for i, a := range args {
// 		works[i] = &Work{Title: template.HTML(a[0])}
// 	}

// 	updateMarkup(works)

// 	for i, w := range works {
// 		if string(w.Title) != args[i][1] {
// 			t.Errorf("want %v, got %v", args[i][1], w.Title)
// 		}
// 	}
// }

func TestFetchWorks(t *testing.T) {
	args := [][]string{
		//[]string{"https://orcid.org/0000-0002-0183-1282", "0000-0002-0183-1282"},
		[]string{"https://orcid.org/0000-0003-1928-5141", "0000-0003-1928-5141"},
		// []string{"https://orcid.org//0000-0002-0183-1282", "0000-0002-0183-1282"},
		// []string{"http://orcid.org/0000-0002-0183-1282", "0000-0002-0183-1282"},
		// []string{"https://orcid.org/0000-0002-0183-1282/", "0000-0002-0183-1282"},

		// TODO: Failed
		// []string{"orcid.org/0000-0002-0183-1282", "0000-0002-0183-1282"},
	}

	var logger = log.New(os.Stdout, "", log.LstdFlags)

	for i, arg := range args {
		var reg *Registry
		var err error

		t.Run(fmt.Sprintf("new %d", i), func(t *testing.T) {
			reg, err = New(arg[0])
			if err != nil {
				t.Error(err)
			}
			if id := reg.UserID(); id != arg[1] {
				t.Errorf("want %v, got %v", arg[1], id)
			}
		})

		t.Run(fmt.Sprintf("fetch %d", i), func(t *testing.T) {
			works, err := reg.FetchWorks(logger)
			if err != nil {
				t.Error(err)
			}
			if len(works) == 0 {
				t.Error("amount of works must be bigger than zero")
			}
		})
	}
}

func TestRender(t *testing.T) {
	paths := []string{
		//"0000-0003-1928-5141.xml",
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
	t.Logf("output: %v", markup)
	if err != nil {
		t.Error(err)
	}
	if len(markup) == 0 {
		t.Error("length of markup must be greater than zero")
	}
}

// func TestWorkType(t *testing.T) {
// 	paths := []string{
// 		"testdata/works.xml",
// 	}

// 	f, err := os.Open(paths[0])
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	defer f.Close()

// 	works, err := decodeWorks(f)
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	if works == nil {
// 		t.Error("works must not be nil")
// 	}

// 	for _, w := range *works {
// 		fmt.Println(w.Type)
// 	}

// }
