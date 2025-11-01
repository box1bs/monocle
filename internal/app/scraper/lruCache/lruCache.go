package lrucache

import "sync"

type LRUCache struct {
	mp         map[[32]byte]*node
	head, tail *node
	mu         *sync.Mutex
	capacity   int
}

type node struct {
	prev, next *node
	key        [32]byte
	value      any
}

func NewLRUCache(cap int) *LRUCache {
	head, tail := newNode([32]byte{}, nil), newNode([32]byte{}, nil)
	head.next = tail
	tail.prev = head
	return &LRUCache{
		mp: 		make(map[[32]byte]*node),
		head:     	head,
		tail:     	tail,
		mu: 		new(sync.Mutex),
		capacity: 	cap,
	}
}

func newNode(key [32]byte, val any) *node {
	return &node{
		key:   key,
		value: val,
	}
}

func (lru *LRUCache) Get(key [32]byte) any {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if v, ex := lru.mp[key]; ex {
		lru.remove(v)
		lru.insert(v)
		return v.value
	}

	return nil
}

func (lru *LRUCache) Put(key [32]byte, value any) {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	
	if _, ok := lru.mp[key]; ok {
		lru.remove(lru.mp[key])
	}

	if len(lru.mp) == lru.capacity {
		lru.remove(lru.tail.prev)
	}

	lru.insert(newNode(key, value))
}

func (lru *LRUCache) remove(node *node) {
	delete(lru.mp, node.key)
	node.prev.next = node.next
	node.next.prev = node.prev
}

func (lru *LRUCache) insert(node *node) {
	lru.mp[node.key] = node
	next := lru.head.next
	lru.head.next = node
	node.prev = lru.head
	next.prev = node
	node.next = next
}