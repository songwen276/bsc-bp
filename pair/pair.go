package pair

import (
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/pair/mysqldb"
	"github.com/ethereum/go-ethereum/pair/pairtypes"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jmoiron/sqlx"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var stateObjectCacheMap *sync.Map

var pairCache = &pairtypes.PairCache{
	TriangleMap:     make(map[int64]pairtypes.Triangle, 2000000),
	PairTriangleMap: make(map[string]pairtypes.Set, 2000000),
}

var abiStr = "[{\"inputs\":[],\"name\":\"arb_wcnwzblucpyf\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"token0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair2\",\"type\":\"address\"}],\"internalType\":\"structITriangularArbitrage.Triangular\",\"name\":\"t\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"startRatio\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"endRatio\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"pieces\",\"type\":\"uint256\"}],\"name\":\"arbitrageQuery\",\"outputs\":[{\"internalType\":\"int256[]\",\"name\":\"roi\",\"type\":\"int256[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"token0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair1\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"router2\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pair2\",\"type\":\"address\"}],\"internalType\":\"structITriangularArbitrage.Triangular\",\"name\":\"t\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"threshold\",\"type\":\"uint256\"}],\"name\":\"isTriangularValid\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"

var ABI *abi.ABI

var LatestBlockNumber = rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)

var From = common.HexToAddress("0xcdecF7Ab7c6654139F65c6C1C7Ecbad653F0dfB0")

var To = common.HexToAddress("0x84F7f6016e5ED7819f717994225D4f60c7Af5359")

func init() {
	// 初始化triange到内存
	triangleStart := time.Now()
	fetchTriangleMap()
	fmt.Printf("初次加载triange到内存中耗时：%v，共加载%v条，加载pair共%v条\n", time.Since(triangleStart), len(pairCache.TriangleMap), len(pairCache.PairTriangleMap))

	// 初始化topic到内存
	topicStart := time.Now()
	fetchTopicMap()
	fmt.Printf("初次加载topic到内存中耗时：%v\n", time.Since(topicStart))

	// 开启协程周期更新内存中triange与topic
	err := gopool.Submit(timerGetTriangle)
	if err != nil {
		fmt.Printf("开启定时加载Triangle任务失败，err=%v\n", err)
		return
	}
	err = gopool.Submit(timerGetTopic)
	if err != nil {
		fmt.Printf("开启定时加载Topic任务失败，err=%v\n", err)
		return
	}

	// 加载三角合约abi
	if parsed, err := abi.JSON(strings.NewReader(abiStr)); err != nil {
		fmt.Printf("加载三角合约abi失败，err=%v\n", err)
		return
	} else {
		ABI = &parsed
	}
	fmt.Printf("初次加载三角合约abi到内存中成功：%v\n", *ABI)

}

func GetPairControl() *pairtypes.PairCache {
	return pairCache
}

func GetStateObjectCacheMap() *sync.Map {
	return stateObjectCacheMap
}

func timerGetTriangle() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fetchTriangleMap()
		}
	}
}

func timerGetTopic() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fetchTopicMap()
		}
	}
}

func fetchTopicMap() {
	// 读取文件内容
	start := time.Now()
	fileContent, err := os.ReadFile("/bc/bsc/build/bin/topic.json")
	if err != nil {
		log.Error("Failed to read file", "err", err)
	}

	// 解析 JSON 文件内容到 map
	newTopicMap := make(map[string]string)
	err = json.Unmarshal(fileContent, &newTopicMap)
	if err != nil {
		log.Error("Failed to unmarshal JSON", "err", err)
	}
	pairCache.TopicMap = newTopicMap
	log.Info("刷新内存中topic耗时", "time", time.Since(start))
}

func fetchTriangleMap() {
	// 初始化数据库连接
	printMemUsed()
	start := time.Now()
	mysqlDB := mysqldb.GetMysqlDB()

	// 使用流式查询，逐行处理数据
	rows, err := mysqlDB.Queryx("SELECT id, token0, router0, pair0, token1, router1, pair1, token2, router2, pair2 FROM arbitrage_triangle")
	if err != nil {
		log.Error("查询失败", "err", err)
	}
	defer func(rows *sqlx.Rows) {
		err := rows.Close()
		if err != nil {
			log.Error("流式查询关闭rows失败", "err", err)
		}
	}(rows)

	// 遍历查询结果
	for rows.Next() {
		triangle := pairtypes.Triangle{}
		err := rows.StructScan(&triangle)
		if err != nil {
			log.Error("填充结果到结构体失败", "err", err)
		}
		triangle.Pair0 = common.HexToAddress(triangle.Pair0).Hex()
		triangle.Pair1 = common.HexToAddress(triangle.Pair1).Hex()
		triangle.Pair2 = common.HexToAddress(triangle.Pair2).Hex()
		pairCache.TriangleMap[triangle.ID] = triangle
		addTriangleIdToPairTriangleMap(triangle.Pair0, triangle.ID)
		addTriangleIdToPairTriangleMap(triangle.Pair1, triangle.ID)
		addTriangleIdToPairTriangleMap(triangle.Pair2, triangle.ID)
	}

	// 检查是否有遍历中的错误
	if err := rows.Err(); err != nil {
		log.Error("查询失败", "err", err)
	}
	log.Info("刷新内存中triange耗时", "time", time.Since(start), "triange总数", len(pairCache.TriangleMap), "pair总数", len(pairCache.PairTriangleMap))
	printMemUsed()
}

var i = 0

func addTriangleIdToPairTriangleMap(pair string, id int64) {
	if pairSet, exists := pairCache.PairTriangleMap[pair]; exists {
		pairSet.Add(id)
	} else {
		pairSet := make(pairtypes.Set)
		pairSet.Add(id)
		pairCache.PairTriangleMap[pair] = pairSet
	}
}

func printMemUsed() {
	// 读取 /proc/meminfo 文件
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		fmt.Printf("Error reading /proc/meminfo：%v\n", err)
		return
	}

	// 解析内容
	lines := strings.Split(string(data), "\n")
	memInfo := make(map[string]int64)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.Trim(fields[0], ":")
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			memInfo[key] = value
		}
	}

	// 计算总缓存内存
	totalCache := memInfo["Buffers"] + memInfo["Cached"]

	// 输出总内存、空闲内存、可用内存和总缓存内存
	fmt.Printf("Total RAM: %d MB\n", memInfo["MemTotal"]/1024)
	fmt.Printf("Free RAM: %d MB\n", memInfo["MemFree"]/1024)
	fmt.Printf("Available RAM: %d MB\n", memInfo["MemAvailable"]/1024)
	fmt.Printf("Total Cached RAM (Buffers + Cached): %d MB\n", totalCache/1024)
}

func Encoder(name string, args ...interface{}) ([]byte, error) {
	return ABI.Pack(name, args...)
}
