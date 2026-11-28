package deploymentdir

import "os"

type WalkMode uint8

const (
	Existing WalkMode = iota
	ExistingParent
	Create
)

const GaleraUID = 999

type Directory struct {
	*os.File
	path string
}

func (d *Directory) Path() string { return d.path }
