package etcd

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type Store struct {
	client *clientv3.Client
}

func NewStore(client *clientv3.Client) *Store {
	return &Store{client: client}
}

func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if len(resp.Kvs) == 0 {
		return nil, false, nil
	}
	return resp.Kvs[0].Value, true, nil
}

func (s *Store) Put(ctx context.Context, key string, value []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.client.Put(ctx, key, string(value))
	return err
}

func (s *Store) Watch(ctx context.Context, key string) clientv3.WatchChan {
	return s.client.Watch(ctx, key)
}
