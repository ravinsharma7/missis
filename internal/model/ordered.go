package model

import (
	"fmt"
	"sort"
	"strconv"
)

const orderKeyWidth = 20

// OrderKeyForIndex creates a sparse, lexicographically sortable key. The
// spacing leaves room for ordinary inserts while the key remains opaque to
// clients; only the core creates it.
func OrderKeyForIndex(index int) string {
	if index < 0 {
		index = 0
	}
	return fmt.Sprintf("%0*d", orderKeyWidth, (index+1)*1000000)
}

// BetweenOrderKeys returns a key strictly between two existing keys. Empty
// bounds mean the beginning or end of a stream. When a dense interval has no
// representable decimal midpoint, the caller should allocate a fresh sparse
// key for the sibling stream.
func BetweenOrderKeys(left, right string) (string, error) {
	leftValue, leftOK := parseOrderKey(left)
	rightValue, rightOK := parseOrderKey(right)
	if left != "" && !leftOK || right != "" && !rightOK {
		return "", fmt.Errorf("invalid order key bounds")
	}
	if !leftOK {
		leftValue = 0
	}
	if !rightOK {
		rightValue = int64(^uint64(0) >> 1)
	}
	if rightOK && leftValue >= rightValue {
		return "", fmt.Errorf("order key bounds are not ordered")
	}
	middle := (leftValue + rightValue) / 2
	if middle <= leftValue || (rightOK && middle >= rightValue) {
		return "", fmt.Errorf("order key interval is full")
	}
	return fmt.Sprintf("%0*d", orderKeyWidth, middle), nil
}

func parseOrderKey(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed >= 0
}

// OrderedChildren returns current children in containment order. Legacy
// events without a key retain deterministic stream-sequence/ID ordering via
// CreatedSequence and CreatedBy.
func OrderedChildren(proj *Projection, parent *PartID) []*Part {
	if proj == nil {
		return nil
	}
	children := make([]*Part, 0)
	for _, part := range proj.Parts {
		if samePartParent(part.ParentID, parent) {
			children = append(children, part)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		left, right := children[i], children[j]
		if left.OrderKey != "" || right.OrderKey != "" {
			if left.OrderKey == "" {
				return false
			}
			if right.OrderKey == "" {
				return true
			}
			if left.OrderKey != right.OrderKey {
				return left.OrderKey < right.OrderKey
			}
		}
		if left.CreatedSequence != right.CreatedSequence {
			return left.CreatedSequence < right.CreatedSequence
		}
		return left.ID < right.ID
	})
	return children
}

func samePartParent(left, right *PartID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// OrderedPartIDs performs a parent-before-child traversal for exports and
// renderers. It does not execute or interpret any Part value.
func OrderedPartIDs(proj *Projection) []PartID {
	if proj == nil {
		return nil
	}
	var result []PartID
	var visit func(*PartID)
	visit = func(parent *PartID) {
		for _, part := range OrderedChildren(proj, parent) {
			result = append(result, part.ID)
			id := part.ID
			visit(&id)
		}
	}
	visit(nil)
	return result
}
