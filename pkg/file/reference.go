package file

import (
	"fmt"
)

var nextID = 0

// ID is used for file tree manipulation to uniquely identify tree nodes.
type ID uint64

// Reference represents a unique file. This is useful when path is not good enough (i.e. you have the same file path for two files in two different container image layers, and you need to be able to distinguish them apart)
type Reference struct {
	id   ID
	Path Path
}

// NewFileReference creates a new unique file reference for the given path.
func NewFileReference(path Path) Reference {
	nextID++
	return Reference{
		Path: path,
		id:   ID(nextID),
	}
}

func NewFileReferenceWithID(path Path, id uint64) Reference {
	return Reference{
		Path: path,
		id:   ID(id),
	}
}

// ID returns the unique ID for this file reference.
func (f *Reference) ID() ID {
	return f.id
}

// String returns a string representation of the path with a unique ID.
func (f *Reference) String() string {
	return fmt.Sprintf("[%v] %v", f.id, f.Path)
}
