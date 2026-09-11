package translate

import (
	"sort"
	"strings"
	"testing"
)

func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "ch" + string(rune('a'+i)) + ".xhtml"
	}
	return out
}

func chosen(t *testing.T, only map[string]bool) string {
	t.Helper()
	var out []string
	for k := range only {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestSelectDocumentsReadsAChapterSpec(t *testing.T) {
	all := names(9) // cha … chi
	for _, c := range []struct{ spec, want string }{
		{"1", "cha.xhtml"},
		{"3", "chc.xhtml"},
		{"1-3", "cha.xhtml,chb.xhtml,chc.xhtml"},
		{"1-3,7", "cha.xhtml,chb.xhtml,chc.xhtml,chg.xhtml"},
		{"8-", "chh.xhtml,chi.xhtml"},
		{" 2 , 4 ", "chb.xhtml,chd.xhtml"},
		{"2,2,2", "chb.xhtml"}, // a repeat is not two chapters
		{"1-9", "cha.xhtml,chb.xhtml,chc.xhtml,chd.xhtml,che.xhtml,chf.xhtml,chg.xhtml,chh.xhtml,chi.xhtml"},
	} {
		only, err := SelectDocuments(all, c.spec)
		if err != nil {
			t.Errorf("%q: %v", c.spec, err)
			continue
		}
		if got := chosen(t, only); got != c.want {
			t.Errorf("%q selected %s, want %s", c.spec, got, c.want)
		}
	}
}

func TestAnEmptySpecMeansTheWholeBook(t *testing.T) {
	// nil, not a map holding every name: BookOptions.Only reads an empty map
	// as "no restriction", and saying it that way keeps the two cases apart.
	for _, spec := range []string{"", "   "} {
		only, err := SelectDocuments(names(4), spec)
		if err != nil {
			t.Fatalf("%q: %v", spec, err)
		}
		if only != nil {
			t.Errorf("%q selected %v, want no restriction at all", spec, only)
		}
	}
}

func TestABadChapterSpecIsRefusedRatherThanGuessed(t *testing.T) {
	// Silently dropping a number nobody can act on is how a run comes back
	// having translated the wrong chapter, or none, an hour later.
	all := names(5)
	for _, spec := range []string{
		"0",    // chapters are numbered from one, as the report shows them
		"6",    // past the end
		"1-6",  // range past the end
		"4-2",  // backwards
		"deux", // not a number
		"1-x",  // not a number either
		"-3",   // no lower bound
		",",    // nothing at all
		"1--3", // malformed
		"1;2",  // wrong separator
	} {
		if only, err := SelectDocuments(all, spec); err == nil {
			t.Errorf("%q was accepted and selected %v", spec, only)
		}
	}
}

func TestChapterNumbersFollowTheOrderTheRunReports(t *testing.T) {
	// The numbers mean positions in the list the run walks, not archive paths
	// and not spine order read some other way. That is what makes "chapter 4
	// failed" answerable with "--chapters 4".
	all := []string{"OEBPS/preface.xhtml", "OEBPS/ch1.xhtml", "OEBPS/nav.xhtml"}
	only, err := SelectDocuments(all, "2")
	if err != nil {
		t.Fatal(err)
	}
	if !only["OEBPS/ch1.xhtml"] || len(only) != 1 {
		t.Errorf("selected %v, want the second document of the list", only)
	}
}
