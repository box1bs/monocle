package tree

import parser "github.com/box1bs/Saturday/pkg/robots_parser"

type TreeNode struct {
	url    		string
	children 	[]*TreeNode
	rule 		*parser.RobotsTxt
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

func (n *TreeNode) GetRules() *parser.RobotsTxt {
	return n.rule
}

func (n *TreeNode) SetRules(rules *parser.RobotsTxt) {
	n.rule = rules
}