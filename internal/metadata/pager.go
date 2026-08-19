package metadata

// pager owns the two decisions every paginated listing has to make together:
// when to stop, and which marker to hand back.
//
// S3 resumes a listing strictly AFTER the marker, so the marker must name the
// last key that was RETURNED. Naming the first key that was not returned — the
// obvious-looking choice at the point where the loop breaks — silently drops
// exactly one key per page. Keeping both decisions here means a listing cannot
// express the wrong one.
type pager struct {
	maxKeys   int
	returned  int
	lastKey   string
	truncated bool
}

func newPager(maxKeys int) *pager {
	return &pager{maxKeys: maxKeys}
}

// full reports whether the page is complete. A listing checks this before
// consuming the next key, and marks itself truncated when it stops.
func (p *pager) full() bool {
	if p.maxKeys > 0 && p.returned >= p.maxKeys {
		p.truncated = true
		return true
	}
	return false
}

// took records a key that is going into the response.
func (p *pager) took(key string) {
	p.returned++
	p.lastKey = key
}

// nextMarker is empty unless the listing stopped early, in which case it names
// the last key returned.
func (p *pager) nextMarker() string {
	if !p.truncated {
		return ""
	}
	return p.lastKey
}

func (p *pager) isTruncated() bool { return p.truncated }
