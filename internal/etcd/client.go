package etcd

import (
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func NewClient(endpoints []string) (*clientv3.Client, error) {
	return clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
}
