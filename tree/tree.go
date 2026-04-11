package tree

import (
	"strings"
)

type PathConfig struct {
	Path      string
	PathType  string
	Key       string
	Port      int32
	Algorithm string
}

type SegmentNode struct {
	Children map[string]*SegmentNode
	Config   *PathConfig
}

func NewSegmentNode() *SegmentNode {
	return &SegmentNode{
		Children: make(map[string]*SegmentNode),
	}
}

func splitPath(url string) []string {
	if url == "" {
		return []string{}
	}
	parts := strings.Split(url, "/")
	res := make([]string, 0, len(parts))
	for _, urlPath := range parts {
		if urlPath != "" {
			res = append(res, urlPath)
		}
	}
	return res
}

func (node *SegmentNode) Insert(path string, config *PathConfig) {
	segments := splitPath(path)
	current := node
	for _, segment := range segments {
		if current.Children == nil {
			current.Children = make(map[string]*SegmentNode)
		}

		child, exists := current.Children[segment]
		if !exists {
			child = &SegmentNode{
				Children: make(map[string]*SegmentNode),
			}
			current.Children[segment] = child
		}
		current = child
	}
	current.Config = config
}

func (node *SegmentNode) Delete(path string) {
	segments := splitPath(path)
	current := node
	for _, segment := range segments {
		if current.Children == nil {
			return
		}
		child, exists := current.Children[segment]
		if !exists {
			return
		}

		current = child
	}
	current.Config = nil
}

func (node *SegmentNode) Match(path string) *PathConfig {
	segments := splitPath(path)
	current := node
	var bestMatch *PathConfig
	if current.Config != nil && current.Config.PathType == "Prefix" {
		bestMatch = current.Config
	}
	for i, segment := range segments {
		child, exists := current.Children[segment]
		if !exists {
			break
		}
		current = child
		if current.Config != nil {
			if current.Config.PathType == "Exact" && i == len(segments)-1 {
				return current.Config
			}
			if current.Config.PathType == "Prefix" {
				bestMatch = current.Config
			}
		}
	}
	return bestMatch
}
