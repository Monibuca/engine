package track

type Channel[T any] struct {
	listeners []chan T
}

func (r *Channel[T]) CreateReader(l int) chan T {
	c := make(chan T, l)
	r.listeners = append(r.listeners, c)
	return c
}

func (r *Channel[T]) AddListener(c chan T) {
	r.listeners = append(r.listeners, c)
}

func (r *Channel[T]) Write(data T) {
	for _, listener := range r.listeners {
		if len(listener) == cap(listener) {
			<-listener
		}
		listener <- data
	}
}