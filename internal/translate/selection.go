package translate

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// SelectDocuments turns a written chapter selection into the set BookOptions.Only
// expects: "1-3,7" keeps the first three documents and the seventh.
//
// The numbers are positions in names, which is the order the run reports and
// the order the book screen lists. That is what makes "chapter 4 failed"
// answerable with "--chapters 4" rather than with an archive path nobody has
// seen.
//
// An empty spec selects everything, by returning nil: BookOptions.Only reads a
// nil or empty map as "no restriction".
//
// Nothing is skipped quietly. A number outside the book, or a range written
// backwards, is an error — a typo that translated the wrong chapter, or none,
// would cost a whole run to notice.
func SelectDocuments(names []string, spec string) (map[string]bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	if len(names) == 0 {
		return nil, errors.New(i18n.T("translate.err.no-document"))
	}

	only := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, err := parseRange(part, len(names))
		if err != nil {
			return nil, err
		}
		for i := lo; i <= hi; i++ {
			only[names[i-1]] = true
		}
	}
	if len(only) == 0 {
		return nil, fmt.Errorf(i18n.T("translate.err.chapters-empty"), spec)
	}
	return only, nil
}

// parseRange reads one comma-separated piece: "7", "2-5", or "9-" for
// everything from the ninth on. Both bounds are 1-based and inclusive.
func parseRange(part string, total int) (lo, hi int, err error) {
	bad := func() (int, int, error) {
		return 0, 0, fmt.Errorf(i18n.T("translate.err.chapters-syntax"), part)
	}

	before, after, isRange := strings.Cut(part, "-")
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)

	lo, err = strconv.Atoi(before)
	if err != nil {
		return bad()
	}
	switch {
	case !isRange:
		hi = lo
	case after == "":
		hi = total // "9-" means to the end of the book
	default:
		hi, err = strconv.Atoi(after)
		if err != nil {
			return bad()
		}
	}

	if lo < 1 || hi > total {
		return 0, 0, fmt.Errorf(i18n.T("translate.err.chapters-range"), part, total)
	}
	if lo > hi {
		return 0, 0, fmt.Errorf(i18n.T("translate.err.chapters-backwards"), part)
	}
	return lo, hi, nil
}
