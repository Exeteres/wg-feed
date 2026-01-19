package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNegotiateResponseMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		accept []string
		want   responseMode
	}{
		{name: "no accept is unsupported", accept: nil, want: responseModeOther},
		{name: "explicit json", accept: []string{"application/json"}, want: responseModeJSON},
		{name: "explicit sse", accept: []string{"text/event-stream"}, want: responseModeSSE},
		{name: "multiple values prefer sse", accept: []string{"application/json, text/event-stream"}, want: responseModeSSE},
		{name: "multiple headers prefer sse", accept: []string{"application/json", "text/event-stream"}, want: responseModeSSE},
		{name: "wildcard all is unsupported", accept: []string{"*/*"}, want: responseModeOther},
		{name: "wildcard application is unsupported", accept: []string{"application/*"}, want: responseModeOther},
		{name: "params ignored", accept: []string{"text/event-stream; q=0.5"}, want: responseModeSSE},
		{name: "other media type", accept: []string{"text/html"}, want: responseModeOther},
		{name: "empty header is unsupported", accept: []string{""}, want: responseModeOther},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, "http://example.test/foo", nil)
			r.Header.Del("Accept")
			for _, v := range tc.accept {
				r.Header.Add("Accept", v)
			}

			if got := negotiateResponseMode(r); got != tc.want {
				t.Fatalf("negotiateResponseMode() = %v, want %v", got, tc.want)
			}
		})
	}
}
