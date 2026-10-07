// Package slideplan groups an ordered image list into slides: solo images that
// crop nicely under object-cover, and side-by-side pairs of same-orientation
// outliers whose aspect is too far from the screen to crop well.
package slideplan

// Photo is one image in the playback order. Album is its source album name, zero when the backend has no albums.
type Photo struct {
	Name  string
	Album string
}

// Slide is one displayable unit: a solo image (len(Names) == 1) or a pair.
// Album is the shared album of the slide's images, zero when unknown. A pair
// never mixes albums, so one value describes the whole slide.
type Slide struct {
	Names []string
	Album string
}

// Threshold.Factor is the aspect deviation at which an image becomes an outlier:
// it pairs when its ratio differs from the screen's by >= Factor or <= 1/Factor.
type Threshold struct {
	Factor float64
}

type orientation int

const (
	fit orientation = iota
	tall
	wide
)

func classify(ratio, screen float64, thr Threshold) orientation {
	if screen <= 0 {
		return fit
	}
	dev := ratio / screen
	switch {
	case dev >= thr.Factor:
		return wide
	case dev <= 1/thr.Factor:
		return tall
	default:
		return fit
	}
}

// pairKey scopes the pairing queues to one album, so a slide never mixes two.
// The year is part of the album, so two photos of one album always share a key.
type pairKey struct {
	o     orientation
	album string
}

// held is an outlier waiting for a partner of the same orientation and album.
type held struct {
	key  pairKey
	name string
}

// Plan groups order into slides. Outliers of the same orientation and album
// pair in order; a leftover re-pairs with the previous same-orientation outlier
// of its album, except a lone outlier of its kind, which shows solo. Disabled or
// unknown aspect = all solo.
func Plan(order []Photo, screen float64, ratioOf func(string) (float64, bool), thr Threshold, enabled bool) []Slide {
	var slides []Slide
	if !enabled {
		for _, p := range order {
			slides = append(slides, Slide{Names: []string{p.Name}, Album: p.Album})
		}
		return slides
	}

	var pending []held                 // held outliers in arrival order
	lastPaired := map[pairKey]string{} // most recent outlier consumed into a pair, per key

	for _, p := range order {
		ratio, known := ratioOf(p.Name)
		o := fit
		if known {
			o = classify(ratio, screen, thr)
		}
		if o == fit {
			slides = append(slides, Slide{Names: []string{p.Name}, Album: p.Album})
			continue
		}
		key := pairKey{o: o, album: p.Album}
		if i := heldIndex(pending, key); i >= 0 {
			slides = append(slides, Slide{Names: []string{pending[i].name, p.Name}, Album: p.Album})
			pending = append(pending[:i], pending[i+1:]...)
			lastPaired[key] = p.Name
			continue
		}
		pending = append(pending, held{key: key, name: p.Name})
	}

	// Arrival order, so an unpaired outlier keeps its place relative to the
	// others it trailed.
	for _, h := range pending {
		if prev, ok := lastPaired[h.key]; ok {
			slides = append(slides, Slide{Names: []string{prev, h.name}, Album: h.key.album})
			continue
		}
		slides = append(slides, Slide{Names: []string{h.name}, Album: h.key.album})
	}

	return slides
}

func heldIndex(pending []held, key pairKey) int {
	for i, h := range pending {
		if h.key == key {
			return i
		}
	}
	return -1
}
