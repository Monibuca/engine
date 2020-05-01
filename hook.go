package engine

var AuthHooks = make(AuthHook, 0)

type AuthHook []func(string) error

func (h AuthHook) AddHook(hook func(string) error) {
	AuthHooks = append(h, hook)
}
func (h AuthHook) Trigger(sign string) error {
	for _, f := range h {
		if err := f(sign); err != nil {
			return err
		}
	}
	return nil
}

var OnPublishHooks = make(OnPublishHook, 0)

type OnPublishHook []func(r *Stream)

func (h OnPublishHook) AddHook(hook func(r *Stream)) {
	OnPublishHooks = append(h, hook)
}
func (h OnPublishHook) Trigger(r *Stream) {
	for _, f := range h {
		f(r)
	}
}

var OnSubscribeHooks = make(OnSubscribeHook, 0)

type OnSubscribeHook []func(s *Subscriber)

func (h OnSubscribeHook) AddHook(hook func(s *Subscriber)) {
	OnSubscribeHooks = append(h, hook)
}
func (h OnSubscribeHook) Trigger(s *Subscriber) {
	for _, f := range h {
		f(s)
	}
}

var OnUnSubscribeHooks = make(OnUnSubscribeHook, 0)

type OnUnSubscribeHook []func(s *Subscriber)

func (h OnUnSubscribeHook) AddHook(hook func(s *Subscriber)) {
	OnUnSubscribeHooks = append(h, hook)
}
func (h OnUnSubscribeHook) Trigger(s *Subscriber) {
	for _, f := range h {
		f(s)
	}
}

var OnDropHooks = make(OnDropHook, 0)

type OnDropHook []func(s *Subscriber)

func (h OnDropHook) AddHook(hook func(s *Subscriber)) {
	OnDropHooks = append(h, hook)
}
func (h OnDropHook) Trigger(s *Subscriber) {
	for _, f := range h {
		f(s)
	}
}

var OnSummaryHooks = make(OnSummaryHook, 0)

type OnSummaryHook []func(bool)

func (h OnSummaryHook) AddHook(hook func(bool)) {
	OnSummaryHooks = append(h, hook)
}
func (h OnSummaryHook) Trigger(v bool) {
	for _, f := range h {
		f(v)
	}
}

var OnStreamClosedHooks = make(OnStreamClosedHook, 0)

type OnStreamClosedHook []func(*Stream)

func (h OnStreamClosedHook) AddHook(hook func(*Stream)) {
	OnStreamClosedHooks = append(h, hook)
}
func (h OnStreamClosedHook) Trigger(v *Stream) {
	for _, f := range h {
		f(v)
	}
}
