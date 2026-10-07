package slideshow

import (
	"github.com/MateEke/picture-frame/internal/library"
	"github.com/MateEke/picture-frame/internal/slideplan"
)

type librarySource struct {
	lib *library.Library
}

// NewLibrarySource returns a slideplan.Source backed by lib.
func NewLibrarySource(lib *library.Library) slideplan.Source {
	return librarySource{lib: lib}
}

func (s librarySource) Order() []slideplan.Photo {
	return photos(s.lib.Cycle())
}

func (s librarySource) NextCycle() []slideplan.Photo {
	return photos(s.lib.Reshuffle())
}

func photos(images []library.Image) []slideplan.Photo {
	out := make([]slideplan.Photo, len(images))
	for i, img := range images {
		out[i] = slideplan.Photo{Name: img.Name, Album: img.Album}
	}
	return out
}
