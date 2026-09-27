package llm

import (
	"context"
	"sync"
	"time"
)

func init() {
	Register("fake", func(ProviderConfig) Client { return &FakeClient{} })
}

// FakeClient — для тестов и разработки без ключа. По умолчанию отвечает «бензовоза нет».
// Next позволяет задать ответы по очереди.
type FakeClient struct {
	mu       sync.Mutex
	Next     []Response
	Err      []error
	Requests []Request
}

const fakeNoTanker = `{"tanker_present":false,"confidence":0.95,"tanker_in_zone":false,"view_matches_reference":true,"view_obstructed":false,"note":"fake: бензовоза нет"}`

func (f *FakeClient) Complete(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)
	if len(f.Err) > 0 {
		err := f.Err[0]
		f.Err = f.Err[1:]
		if err != nil {
			return Response{}, err
		}
	}
	if len(f.Next) > 0 {
		r := f.Next[0]
		f.Next = f.Next[1:]
		return r, nil
	}
	return Response{Text: fakeNoTanker, Model: "fake", Latency: time.Millisecond}, nil
}
