package pairtypes

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"strings"
)

type PairAPI interface {
	BlockChainCallBatch(datas [][]byte) (string, error)
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

type TransactionArgs struct {
	From                 *common.Address `json:"from"`
	To                   *common.Address `json:"to"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	GasPrice             *hexutil.Big    `json:"gasPrice"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	Value                *hexutil.Big    `json:"value"`
	Nonce                *hexutil.Uint64 `json:"nonce"`

	// We accept "data" and "input" for backwards-compatibility reasons.
	// "input" is the newer name and should be preferred by clients.
	// Issue detail: https://github.com/ethereum/go-ethereum/issues/15628
	Data  *hexutil.Bytes `json:"data"`
	Input *hexutil.Bytes `json:"input"`

	// Introduced by AccessListTxType transaction.
	AccessList *types.AccessList `json:"accessList,omitempty"`
	ChainID    *hexutil.Big      `json:"chainId,omitempty"`

	// For BlobTxType
	BlobFeeCap *hexutil.Big  `json:"maxFeePerBlobGas"`
	BlobHashes []common.Hash `json:"blobVersionedHashes,omitempty"`

	// For BlobTxType transactions with blob sidecar
	Blobs       []kzg4844.Blob       `json:"blobs"`
	Commitments []kzg4844.Commitment `json:"commitments"`
	Proofs      []kzg4844.Proof      `json:"proofs"`

	// This configures whether blobs are allowed to be passed.
	blobSidecarAllowed bool
}
