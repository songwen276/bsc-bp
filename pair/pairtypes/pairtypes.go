package pairtypes

import (
	"fmt"
	"strings"
)

type Triangle struct {
	ID      int64  `db:"id"`
	Token0  string `db:"token0"`
	Router0 string `db:"router0"`
	Pair0   string `db:"pair0"`
	Token1  string `db:"token1"`
	Router1 string `db:"router1"`
	Pair1   string `db:"pair1"`
	Token2  string `db:"token2"`
	Router2 string `db:"router2"`
	Pair2   string `db:"pair2"`
}

type PairCache struct {
	TriangleMap     map[int64]Triangle
	TopicMap        map[string]string
	PairTriangleMap map[string]Set
}

// Set 实现一个set
type Set map[int64]struct{}

// Add 添加元素
func (s Set) Add(value int64) {
	s[value] = struct{}{}
}

// Remove 删除元素
func (s Set) Remove(value int64) {
	delete(s, value)
}

// Contains 检查元素是否存在
func (s Set) Contains(value int64) bool {
	_, exists := s[value]
	return exists
}

// String 方法
func (s Set) String() string {
	var pairs []string
	for k, _ := range s {
		pairs = append(pairs, fmt.Sprintf("%d", k))
	}
	return fmt.Sprintf("[%s] (length: %d)", strings.Join(pairs, ", "), len(pairs))
}
