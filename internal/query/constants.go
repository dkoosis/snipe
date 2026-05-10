package query

// Symbol kind constants used in query results.
const (
	kindFunc   = "func"
	kindStruct = "struct"
)

// Action verb constants used by explain to describe call patterns.
const (
	actionUpdates   = "updates"
	actionPersists  = "persists"
	actionValidates = "validates"
	actionCreates   = "creates"
)
