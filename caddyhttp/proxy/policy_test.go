package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var workableServer *httptest.Server

func TestMain(m *testing.M) {
	workableServer = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// do nothing
		}))
	r := m.Run()
	workableServer.Close()
	os.Exit(r)
}

type customPolicy struct{}

func (r *customPolicy) Select(pool HostPool, request *http.Request) *UpstreamHost {
	return pool[0]
}

func testPool() HostPool {
	pool := []*UpstreamHost{
		{
			Name: workableServer.URL, // this should resolve (healthcheck test)
		},
		{
			Name: "http://localhost:99998", // this shouldn't
		},
		{
			Name: "http://C",
		},
	}
	return HostPool(pool)
}

func TestRoundRobinPolicy(t *testing.T) {
	pool := testPool()
	rrPolicy := &RoundRobin{}
	request, _ := http.NewRequest("GET", "/", nil)

	h := rrPolicy.Select(pool, request)
	// First selected host is 1, because counter starts at 0
	// and increments before host is selected
	if h != pool[1] {
		t.Error("Expected first round robin host to be second host in the pool.")
	}
	h = rrPolicy.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected second round robin host to be third host in the pool.")
	}
	h = rrPolicy.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected third round robin host to be first host in the pool.")
	}
	// mark host as down
	pool[1].Unhealthy = true
	h = rrPolicy.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected to skip down host.")
	}
	// mark host as up
	pool[1].Unhealthy = false

	h = rrPolicy.Select(pool, request)
	if h == pool[2] {
		t.Error("Expected to balance evenly among healthy hosts")
	}
	// mark host as full
	pool[1].Conns = 1
	pool[1].MaxConns = 1
	h = rrPolicy.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected to skip full host.")
	}
}

func TestLeastConnPolicy(t *testing.T) {
	pool := testPool()
	lcPolicy := &LeastConn{}
	request, _ := http.NewRequest("GET", "/", nil)

	pool[0].Conns = 10
	pool[1].Conns = 10
	h := lcPolicy.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected least connection host to be third host.")
	}
	pool[2].Conns = 100
	h = lcPolicy.Select(pool, request)
	if h != pool[0] && h != pool[1] {
		t.Error("Expected least connection host to be first or second host.")
	}
}

func TestCustomPolicy(t *testing.T) {
	pool := testPool()
	customPolicy := &customPolicy{}
	request, _ := http.NewRequest("GET", "/", nil)

	h := customPolicy.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected custom policy host to be the first host.")
	}
}

func TestIPHashPolicy(t *testing.T) {
	pool := testPool()
	ipHash := &IPHash{}
	request, _ := http.NewRequest("GET", "/", nil)
	// We should be able to predict where every request is routed.
	request.RemoteAddr = "172.0.0.1:80"
	h := ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	request.RemoteAddr = "172.0.0.2:80"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	request.RemoteAddr = "172.0.0.3:80"
	h = ipHash.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected ip hash policy host to be the third host.")
	}
	request.RemoteAddr = "172.0.0.4:80"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}

	// we should get the same results without a port
	request.RemoteAddr = "172.0.0.1"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	request.RemoteAddr = "172.0.0.2"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	request.RemoteAddr = "172.0.0.3"
	h = ipHash.Select(pool, request)
	if h != pool[2] {
		t.Error("Expected ip hash policy host to be the third host.")
	}
	request.RemoteAddr = "172.0.0.4"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}

	// we should get a healthy host if the original host is unhealthy and a
	// healthy host is available
	request.RemoteAddr = "172.0.0.1"
	pool[1].Unhealthy = true
	h = ipHash.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected ip hash policy host to be the first host.")
	}

	request.RemoteAddr = "172.0.0.2"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	pool[1].Unhealthy = false

	request.RemoteAddr = "172.0.0.3"
	pool[2].Unhealthy = true
	h = ipHash.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected ip hash policy host to be the first host.")
	}
	request.RemoteAddr = "172.0.0.4"
	h = ipHash.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected ip hash policy host to be the first host.")
	}

	// We should be able to resize the host pool and still be able to predict
	// where a request will be routed with the same IP's used above
	pool = []*UpstreamHost{
		{
			Name: workableServer.URL, // this should resolve (healthcheck test)
		},
		{
			Name: "http://localhost:99998", // this shouldn't
		},
	}
	pool = HostPool(pool)
	request.RemoteAddr = "172.0.0.1:80"
	h = ipHash.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected ip hash policy host to be the first host.")
	}
	request.RemoteAddr = "172.0.0.2:80"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}
	request.RemoteAddr = "172.0.0.3:80"
	h = ipHash.Select(pool, request)
	if h != pool[0] {
		t.Error("Expected ip hash policy host to be the first host.")
	}
	request.RemoteAddr = "172.0.0.4:80"
	h = ipHash.Select(pool, request)
	if h != pool[1] {
		t.Error("Expected ip hash policy host to be the second host.")
	}

	// We should get nil when there are no healthy hosts
	pool[0].Unhealthy = true
	pool[1].Unhealthy = true
	h = ipHash.Select(pool, request)
	if h != nil {
		t.Error("Expected ip hash policy host to be nil.")
	}
}