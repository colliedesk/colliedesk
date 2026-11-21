package helpcenter

// foldAfter is the most collection levels a breadcrumb shows in full. A deeper trail keeps Home, the
// first level and the last two on the line and folds the levels between into "…", so it stays on one
// line on a phone. Three levels (Home plus three links) still fit.
const foldAfter = 3

// crumbTrail is a page's breadcrumb as the layout draws it. Folded holds the levels the line leaves
// out and Path every level, which the "…" opens; both are empty on a trail that is short enough.
type crumbTrail struct {
	Head, Folded, Tail, Path []link
}

// Trail splits the page's breadcrumb (Home first) for the layout. The structured data keeps the full
// trail; only the visible line folds.
func (p page) Trail() crumbTrail {
	if len(p.Crumbs) <= foldAfter+1 {
		return crumbTrail{Head: p.Crumbs}
	}
	last := len(p.Crumbs) - 2
	return crumbTrail{Head: p.Crumbs[:2], Folded: p.Crumbs[2:last], Tail: p.Crumbs[last:], Path: p.Crumbs[1:]}
}
