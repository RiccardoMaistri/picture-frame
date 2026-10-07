package slideshow_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/MateEke/picture-frame/internal/library"
	"github.com/MateEke/picture-frame/internal/slideplan"
	"github.com/MateEke/picture-frame/internal/slideshow"
)

func TestLibrarySourceOrderUsesCycle(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	lib := library.New([]library.Image{{Name: "a.jpg"}, {Name: "b.jpg"}, {Name: "c.jpg"}, {Name: "d.jpg"}}, true, library.WithTestRNG(rng))
	src := slideshow.NewLibrarySource(lib)
	first := src.Order()
	second := src.Order()
	if !slices.Equal(first, second) {
		t.Fatalf("Order not stable: %v vs %v", first, second)
	}
	if slices.Equal(first, ordered("a.jpg", "b.jpg", "c.jpg", "d.jpg")) {
		t.Fatalf("Order returned canonical, expected shuffled cycle")
	}
}

// The album rides along so the planner can pair and label a slide by album.
func TestLibrarySourceCarriesAlbum(t *testing.T) {
	lib := library.New([]library.Image{
		{Name: "a.jpg", Album: "Trip"},
		{Name: "b.jpg"},
	}, false)
	got := slideshow.NewLibrarySource(lib).Order()
	want := []slideplan.Photo{{Name: "a.jpg", Album: "Trip"}, {Name: "b.jpg"}}
	if !slices.Equal(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
}

func ordered(names ...string) []slideplan.Photo {
	out := make([]slideplan.Photo, len(names))
	for i, n := range names {
		out[i] = slideplan.Photo{Name: n}
	}
	return out
}
