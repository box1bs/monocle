package tree

import (
)

type TreeNode struct {
	url    string
	children []*TreeNode
}

func NewNode(url string) *TreeNode {
	return &TreeNode{
		url:    url,
		children: make([]*TreeNode, 0),
	}
}

func (n *TreeNode) AddChild(child *TreeNode) {
	n.children = append(n.children, child)
}