// Package kvstore names the one key-value table the packages keep their small
// state in: *store.Store satisfies it, and a test passes a map.
package kvstore

// Table is the store's key-value table (*store.Store).
type Table interface {
	GetKV(k string) (string, error)
	SetKV(k, v string) error
}
