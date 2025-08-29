package tree

import "github.com/box1bs/Saturday/pkg/parser"

type TreeNode struct {
	rules 		*parser.RobotsTxt
	children 	[]*TreeNode
	url    		string
}

func NewNode(url string) *TreeNode {
	return &TreeNode{
		children: make([]*TreeNode, 0),
		url:    url,
	}
}

func (n *TreeNode) AddChild(child *TreeNode) {
	n.children = append(n.children, child)
}

func (n *TreeNode) GetRules() *parser.RobotsTxt {
	return n.rules
}

func (n *TreeNode) SetRules(rules *parser.RobotsTxt) {
	n.rules = rules
}