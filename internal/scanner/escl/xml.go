package escl

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

const (
	scanNS = "http://schemas.hp.com/imaging/escl/2011/05/03"
	pwgNS  = "http://www.pwg.org/schemas/2010/12/sm"
)

type node struct {
	name     xml.Name
	text     strings.Builder
	children []*node
}

// Decode a small, bounded status tree. Namespace prefixes are immaterial, but
// namespace URIs and direct ancestry are required. DTDs/entities are unsupported;
// encoding/xml never fetches external entities. Unknown job entries fail closed.
func boundedTree(data []byte) *node {
	if len(data) == 0 || len(data) > 1<<20 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *node
	var stack []*node
	nodes, tokens := 0, 0
	for {
		tokens++
		if tokens > 16384 {
			return nil
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch token := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 2048 || len(stack) >= 32 {
				return nil
			}
			n := &node{name: token.Name}
			if len(stack) == 0 {
				if root != nil {
					return nil
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(token)) != "" {
					return nil
				}
			} else {
				n := stack[len(stack)-1]
				n.text.Write(token)
			}
		case xml.Directive:
			return nil
		case xml.ProcInst:
			if token.Target != "xml" || root != nil {
				return nil
			}
		}
	}
	if root == nil || len(stack) != 0 || strings.TrimSpace(root.text.String()) != "" {
		return nil
	}
	return root
}

func idleStatus(data []byte) bool {
	root := boundedTree(data)
	if root == nil || root.name != (xml.Name{Space: scanNS, Local: "ScannerStatus"}) {
		return false
	}
	state, ok := singleText(root, pwgNS, "State")
	if !ok || state != "Idle" {
		return false
	}
	version, ok := singleText(root, pwgNS, "Version")
	if !ok || version == "" {
		return false
	}
	jobs, ok := singleNode(root, scanNS, "Jobs")
	if !ok {
		return false
	}
	if jobs == nil {
		return true
	}
	if strings.TrimSpace(jobs.text.String()) != "" {
		return false
	}
	for _, job := range jobs.children {
		if job.name != (xml.Name{Space: scanNS, Local: "JobInfo"}) {
			return false
		}
		uri, ok := singleText(job, pwgNS, "JobUri")
		if !ok || uri == "" {
			return false
		}
		state, ok := singleText(job, pwgNS, "JobState")
		if !ok {
			return false
		}
		switch state {
		case "Completed", "Canceled", "Aborted":
		default:
			return false
		}
		transfer, ok := singleNode(job, pwgNS, "ImagesToTransfer")
		if !ok {
			return false
		}
		if transfer != nil {
			value, ok := leaf(transfer)
			if !ok {
				return false
			}
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil || n != 0 {
				return false
			}
		}
	}
	return true
}
func singleNode(parent *node, namespace, local string) (*node, bool) {
	var found *node
	for _, child := range parent.children {
		if child.name == (xml.Name{Space: namespace, Local: local}) {
			if found != nil {
				return nil, false
			}
			found = child
		}
	}
	return found, true
}
func leaf(n *node) (string, bool) {
	if n == nil || len(n.children) != 0 {
		return "", false
	}
	return strings.TrimSpace(n.text.String()), true
}
func singleText(parent *node, namespace, local string) (string, bool) {
	n, ok := singleNode(parent, namespace, local)
	if !ok {
		return "", false
	}
	return leaf(n)
}
