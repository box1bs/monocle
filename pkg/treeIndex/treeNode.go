package tree

import (
	"sync"
)

type TreeNode struct {
	url    string
	childs []*TreeNode
}

var mu sync.Mutex

func NewNode(url string) *TreeNode {
	return &TreeNode{
		url:    url,
		childs: make([]*TreeNode, 0),
	}
}

func (n *TreeNode) AddChild(child *TreeNode) {
	mu.Lock()
	defer mu.Unlock()
	n.childs = append(n.childs, child)
}