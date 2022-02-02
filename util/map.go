package util

import "sync"

type Map[K comparable, V any] struct {
	sync.RWMutex
	Map map[K]V
}

func (m *Map[K, V]) Init() {
	m.Map = make(map[K]V)
}

func (m *Map[K, V]) Add(k K, v V) bool {
	m.Lock()
	defer m.Unlock()
	if _, ok := m.Map[k]; ok {
		return false
	}
	m.Map[k] = v
	return true
}

func (m *Map[K, V]) Set(k K, v V) {
	m.Lock()
	m.Map[k] = v
	m.Unlock()
}

func (m *Map[K, V]) Has(k K) (ok bool) {
	m.RLock()
	defer m.RUnlock()
	_, ok = m.Map[k]
	return
}

func (m *Map[K, V]) Len() int {
	m.RLock()
	defer m.RUnlock()
	return len(m.Map)
}

func (m *Map[K, V]) Get(k K) V {
	m.RLock()
	defer m.RUnlock()
	return m.Map[k]
}

func (m *Map[K, V]) Delete(k K) {
	m.Lock()
	delete(m.Map, k)
	m.Unlock()
}

func (m *Map[K, V]) ToList() (r []V) {
	m.RLock()
	defer m.RUnlock()
	for _, s := range m.Map {
		r = append(r, s)
	}
	return
}

func (m *Map[K, V]) Range(f func(V)) {
	m.RLock()
	defer m.RUnlock()
	for _, s := range m.Map {
		f(s)
	}
}
