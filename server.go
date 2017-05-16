package httpt

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Method is an HTTP method.
type Method string

const (
	// ANY matches with any HTTP method.
	ANY     = Method("")
	CONNECT = Method("CONNECT")
	DELETE  = Method("DELETE")
	GET     = Method("GET")
	HEAD    = Method("HEAD")
	OPTIONS = Method("OPTIONS")
	PATCH   = Method("PATCH")
	POST    = Method("POST")
	PUT     = Method("PUT")
	TRACE   = Method("TRACE")

	// AnyPath matches will all request paths.
	AnyPath = "!AnyPath!"
)

// RoundTripFunc is an function to mock HTTP request-response round trip.
// It is loaded into standard http.Client as a mock transport.
type RoundTripFunc func(*http.Request) (*http.Response, error)

// Server is an test HTTP server that is able to stack multiple roundtrips for any test case.
// Example usage:
//    s := httpt.NewWithT(t)
//    s.Push(StringResponse(http.StatusBadRequest, "really bad request"))
//    ...
//    // Make sure your component uses mocked http e.g passed in context:
//    ctx = context.WithValue(ctx, oauth2.HTTPClient, s.HTTTPClient())
//
//    // Or used directly:
//    resp, err := s.HTTPClient().Do(request)
//
type Server struct {
	*tripBuilder

	DefaultRoundTrip RoundTripFunc
}

// New constructs Server without any default round trip function.
func New() *Server {
	return &Server{
		tripBuilder: newTripBuilder(),
	}
}

// NotMockedFunc is a round trip function that fails Go test. It is used if accidentally httpt.Server is used
// but not round trip func was stacked.
func NotMockedFunc(t *testing.T) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		t.Errorf("httpt.Server: RoundTripFunc not mocked for this request %s:%s",
			r.Method, getPathOnly(r))
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
		}, nil
	}
}

// NewWithT constructs Server with NotMockedFunc as default.
// Always use that when running withing go test.
func NewWithT(t *testing.T) *Server {
	return &Server{
		tripBuilder:      newTripBuilder(),
		DefaultRoundTrip: NotMockedFunc(t),
	}
}

// HTTPClient returns standrard http.Client to feed components the needs to be mocked.
func (s *Server) HTTPClient() *http.Client {
	return &http.Client{
		Transport: &transport{s},
	}
}

// Reset resets stacked round trip functions. Nothing is mocked after that.
func (s *Server) Reset() {
	s.engine.reset()
}

// Len returns number of round trip functions (requests) that are mocked.
// Useful example:
//    assert.Equal(t, 0, s.Len()) // at the end of your unit test with httpt.Server, to check if all mocked requests were actually used.
func (s *Server) Len() int {
	return len(s.engine.queue)
}

type tripEntry struct {
	method Method
	path   string
	trip   RoundTripFunc
}

type tripQueue struct {
	queue []tripEntry
}

func (q *tripQueue) push(method Method, path string, r RoundTripFunc) {
	q.queue = append(q.queue, tripEntry{
		method: method,
		path:   path,
		trip:   r,
	})
}

func (q *tripQueue) reset() {
	q.queue = []tripEntry(nil)
}

func (q *tripQueue) pop(method Method, path string) (RoundTripFunc, bool) {
	for i, e := range q.queue {
		if e.method != method && e.method != ANY {
			continue
		}

		if e.path != path && e.path != AnyPath {
			continue
		}

		q.queue = append(q.queue[:i], q.queue[i+1:]...)
		return e.trip, true
	}

	return nil, false
}

func getPathOnly(req *http.Request) string {
	path := req.URL.String()
	if strings.Contains(path, "?") {
		return strings.Split(path, "?")[0]
	}
	return path
}

type tripPusher struct {
	engine *tripQueue
	method Method
	path   string
}

func newTripPusher(engine *tripQueue, method Method, path string) *tripPusher {
	return &tripPusher{
		engine: engine,
		method: method,
		path:   path,
	}
}

// Push adds round trip function to the queue.
func (t *tripPusher) Push(f RoundTripFunc) {
	t.engine.push(t.method, t.path, f)
}

type tripBuilder struct {
	*tripPusher
}

func newTripBuilder() *tripBuilder {
	return &tripBuilder{
		tripPusher: newTripPusher(&tripQueue{}, ANY, AnyPath),
	}
}

// On specifies particular method and path for mocked round trip function.
// Example usage:
//    server.On(httpt.GET, "/path/test").Push(<any round trip function>)
func (t *tripBuilder) On(method Method, path string) *tripPusher {
	return newTripPusher(t.engine, method, path)
}

// FailureFunc is a round trip function that returns error. It can simulate connection error or timeouts.
func FailureFunc(err error) func(*http.Request) (*http.Response, error) {
	return func(_ *http.Request) (*http.Response, error) {
		return nil, err
	}
}

// transport is for hiding transport implementation method that does not need to be public.
type transport struct {
	s *Server
}

// RoundTrip implements Transport for standard http.Client.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	method := Method(req.Method)
	path := getPathOnly(req)

	if r, ok := t.s.engine.pop(method, path); ok {
		return r(req)
	}

	if t.s.DefaultRoundTrip == nil {
		return nil, fmt.Errorf(
			"httpt.Server request not mocked for this request %s:%s", method, path)
	}
	return t.s.DefaultRoundTrip(req)
}