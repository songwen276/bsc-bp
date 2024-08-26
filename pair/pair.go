package pair

import (
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/pair/mysqldb"
	"github.com/ethereum/go-ethereum/pair/pairtypes"
	"github.com/jmoiron/sqlx"
	"os"
	"strconv"
	"strings"
	"time"
)

var pairCache pairtypes.PairCache

func init() {
	// 初始化triange到内存
	triangleStart := time.Now()
	pairCache = pairtypes.PairCache{}
	pairCache.TriangleMap = make(map[int64]pairtypes.Triangle)
	pairCache.PairTriangleMap = make(map[string]pairtypes.Set)
	fetchTriangleMap()
	fmt.Printf("初次加载triange到内存中耗时：%v，共加载%v条\n", time.Since(triangleStart), len(pairCache.TriangleMap))

	// 初始化topic到内存
	topicStart := time.Now()
	fetchTopicMap()
	fmt.Printf("初次加载topic到内存中耗时：%v\n", time.Since(topicStart))

	// 开启协程周期更新内存中triange与topic
	err1 := gopool.Submit(timerGetTriangle)
	if err1 != nil {
		fmt.Printf("开启定时加载Triangle任务失败，err=%v\n", err1)
		return
	}
	err2 := gopool.Submit(timerGetTopic)
	if err2 != nil {
		fmt.Printf("开启定时加载Topic任务失败，err=%v\n", err2)
		return
	}
}

func GetPairControl() pairtypes.PairCache {
	return pairCache
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
	fileContent, err := os.ReadFile("/blockchain/bsc/build/bin/topic.json")
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
		var triangle pairtypes.Triangle
		err := rows.StructScan(&triangle)
		if err != nil {
			log.Error("填充结果到结构体失败", "err", err)
		}
		pairCache.TriangleMap[triangle.ID] = triangle
		pair0Set, pair0Exists := pairCache.PairTriangleMap[triangle.Pair0]
		if !pair0Exists {
			pair0Set = make(pairtypes.Set)
			pairCache.PairTriangleMap[triangle.Pair0] = pair0Set
		}
		pair1Set, pair1Exists := pairCache.PairTriangleMap[triangle.Pair1]
		if !pair1Exists {
			pair1Set = make(pairtypes.Set)
			pairCache.PairTriangleMap[triangle.Pair1] = pair1Set
		}
		pair2Set, pair2Exists := pairCache.PairTriangleMap[triangle.Pair2]
		if !pair2Exists {
			pair2Set = make(pairtypes.Set)
			pairCache.PairTriangleMap[triangle.Pair2] = pair2Set
		}
		pair0Set.Add(triangle.ID)
		pair1Set.Add(triangle.ID)
		pair2Set.Add(triangle.ID)
	}

	// 检查是否有遍历中的错误
	if err := rows.Err(); err != nil {
		log.Error("查询失败", "err", err)
	}
	log.Info("刷新内存中triange耗时", "time", time.Since(start), "triange总数", len(pairCache.TriangleMap))
	printMemUsed()
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
