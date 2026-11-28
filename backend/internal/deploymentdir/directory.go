package deploymentdir

type WalkMode uint8

const (
	Existing WalkMode = iota
	ExistingParent
	Create
)

const GaleraUID = 999
