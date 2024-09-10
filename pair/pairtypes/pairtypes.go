package pairtypes

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"strings"
	"sync"
)

type PairAPI interface {
	PairCallBatch(datas [][]byte) error
}

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

type ITriangularArbitrageTriangular struct {
	Token0  common.Address
	Router0 common.Address
	Pair0   common.Address
	Token1  common.Address
	Router1 common.Address
	Pair1   common.Address
	Token2  common.Address
	Router2 common.Address
	Pair2   common.Address
}

type PairCache struct {
	mu              sync.RWMutex
	TriangleMap     map[int64]Triangle
	PairTriangleMap map[string]*Set
	TopicMap        map[string]string
}

// NewPairCache 创建一个新的 PairCache
func NewPairCache() *PairCache {
	return &PairCache{
		TriangleMap:     make(map[int64]Triangle),
		PairTriangleMap: make(map[string]*Set),
	}
}

// AddTriangle 向 TriangleMap 添加一个 Triangle
func (pc *PairCache) AddTriangle(id int64, triangle Triangle) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.TriangleMap[id] = triangle
}

// AddPairTriangle 向 PairTriangleMap 添加一个元素
func (pc *PairCache) AddPairTriangle(pair string, id int64) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// 如果 key 不存在，则创建一个新的 Set
	if _, exists := pc.PairTriangleMap[pair]; !exists {
		pc.PairTriangleMap[pair] = NewSet()
	}

	// 添加元素到 Set 中
	pc.PairTriangleMap[pair].Add(id)
}

// GetTriangle 安全地从 TriangleMap 中获取 Triangle
func (pc *PairCache) GetTriangle(id int64) (Triangle, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	if triangle, exists := pc.TriangleMap[id]; exists {
		return triangle, true
	} else {
		return triangle, false
	}
}

// GetPairSet 安全地从 PairTriangleMap 中获取 Set
func (pc *PairCache) GetPairSet(pair string) *Set {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	if set, exists := pc.PairTriangleMap[pair]; exists {
		return set
	}
	return nil
}

// TriangleMapSize 返回 TriangleMap 中的元素数量
func (pc *PairCache) TriangleMapSize() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.TriangleMap)
}

// PairTriangleMapSize 返回 PairTriangleMap 中指定 key 的 Set 的元素数量
func (pc *PairCache) PairTriangleMapSize() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.PairTriangleMap)
}

// Set 实现一个set
type Set struct {
	mu sync.RWMutex
	m  map[int64]struct{}
}

// NewSet 创建一个新的线程安全的 Set
func NewSet() *Set {
	return &Set{
		m: make(map[int64]struct{}),
	}
}

// Add 添加一个元素到 Set 中
func (s *Set) Add(item int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[item] = struct{}{}
}

// Size 返回 Set 中元素的数量
func (s *Set) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

// Iterate 遍历 Set 中的所有元素
func (s *Set) Iterate() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 将元素复制到一个切片中返回
	items := make([]int64, 0, len(s.m))
	for item := range s.m {
		items = append(items, item)
	}
	return items
}

// String 方法
func (s Set) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var triangleIdSet []string
	for k, _ := range s.m {
		triangleIdSet = append(triangleIdSet, fmt.Sprintf("%d", k))
	}
	return fmt.Sprintf("[%s] (length: %d)", strings.Join(triangleIdSet, ", "), len(triangleIdSet))
}
