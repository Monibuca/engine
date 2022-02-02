package engine

type Publisher interface {
	OnStateChange(oldState StreamState, newState StreamState) bool
}
