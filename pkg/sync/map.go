package sync

import (
	"sync"
)

// Define a generic, type-safe map
type SyncMap[K comparable, V any] struct {
	m sync.Map
}

// Set a value in the map
func (g *SyncMap[K, V]) Set(key K, value V) {
	g.m.Store(key, value)
}

// Get a value from the map
func (g *SyncMap[K, V]) Get(key K) (V, bool) {
	val, ok := g.m.Load(key)
	if !ok {
		return *new(V), false
	}

	return val.(V), true
}

func (g *SyncMap[K, V]) AsMap() map[K]V {
	normalMap := map[K]V{}

	g.m.Range(func(key any, value any) bool {
		normalMap[key.(K)] = value.(V)
		return true
	})

	return normalMap
}
