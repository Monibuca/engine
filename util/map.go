package util

import "sync"

type Map[K comparable, V any] struct {
	sync.Map
}

func (m *Map[K, V]) Add(k K, v V) bool {
	_, loaded := m.LoadOrStore(k, v)
	return !loaded
}

func (m *Map[K, V]) Set(k K, v V) {
	m.Store(k, v)
}

func (m *Map[K, V]) Has(k K) (ok bool) {
	_, ok = m.Load(k)
	return
}

func (m *Map[K, V]) Len() (l int) {
	m.Map.Range(func(k, v interface{}) bool {
		l++
		return true
	})
	return
}

func (m *Map[K, V]) Get(k K) (result V) {
	v, ok := m.Load(k)
	if !ok {
		return
	}
	return v.(V)
}

func (m *Map[K, V]) Delete(k K) (v V, ok bool) {
	var r any
	if r, ok = m.Map.LoadAndDelete(k); ok {
		v = r.(V)
	}
	return
}

func (m *Map[K, V]) ToList() (r []V) {
	m.Map.Range(func(k, v interface{}) bool {
		r = append(r, v.(V))
		return true
	})
	return
}

func MapList[K comparable, V any, R any](m *Map[K, V], f func(K, V) R) (r []R) {
	m.Map.Range(func(k, v interface{}) bool {
		r = append(r, f(k.(K), v.(V)))
		return true
	})
	return
}

func (m *Map[K, V]) Range(f func(K, V)) {
	m.Map.Range(func(k, v interface{}) bool {
		f(k.(K), v.(V))
		return true
	})
}
