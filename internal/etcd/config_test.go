package etcd

import "testing"

func TestEndpointsFromEnv(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("ETCD_ENDPOINTS", "")
		_, err := EndpointsFromEnv()
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("single", func(t *testing.T) {
		t.Setenv("ETCD_ENDPOINTS", "http://127.0.0.1:2379")
		got, err := EndpointsFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "http://127.0.0.1:2379" {
			t.Fatalf("unexpected endpoints: %#v", got)
		}
	})

	t.Run("comma-separated with spaces", func(t *testing.T) {
		t.Setenv("ETCD_ENDPOINTS", " http://a:2379 , http://b:2379, ")
		got, err := EndpointsFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0] != "http://a:2379" || got[1] != "http://b:2379" {
			t.Fatalf("unexpected endpoints: %#v", got)
		}
	})
}
