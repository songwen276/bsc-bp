package pair

import (
	"encoding/json"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/pair/mysqldb"
	"github.com/ethereum/go-ethereum/pair/types"
	"os"
	"time"
)

var pairControl types.PairControl

func init() {
	pairControl = types.PairControl{}
	pairControl.GetTopicing.Store(false)
	pairControl.GetTriangleing.Store(false)
	pairControl.TriangleMap = make(map[int64]types.Triangle)
	pairControl.PairTriangleMap = make(map[string]types.Set)
	fetchTriangleMap()
	gopool.Submit(timerGetTriangle)
	fetchTopicMap()
	gopool.Submit(timerGetTopic)
}

func GetPairControl() types.PairControl {
	return pairControl
}

func timerGetTriangle() {
	ticker := time.NewTicker(1 * time.Minute)
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
	if pairControl.GetTopicing.Load() {
		log.Error("TopicMap正在加载中，本次直接跳过")
		return
	}
	// 读取文件内容
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
	pairControl.TopicMap = newTopicMap
}

func fetchTriangleMap() {
	if pairControl.GetTriangleing.Load() {
		log.Error("TriangleMap正在加载中，本次直接跳过")
		return
	}
	// 初始化数据库连接
	mysqlDB := mysqldb.GetMysqlDB()

	// 使用流式查询，逐行处理数据
	rows, err := mysqlDB.Queryx("SELECT id, token0, router0, pair0, token1, router1, pair1, token2, router2, pair2 FROM arbitrage_triangle LIMIT 0,10")
	if err != nil {
		log.Error("查询失败", "err", err)
	}
	defer rows.Close()

	// 遍历查询结果
	for rows.Next() {
		var triangle types.Triangle
		err := rows.StructScan(&triangle)
		if err != nil {
			log.Error("填充结果到结构体失败", "err", err)
		}
		pairControl.TriangleMap[triangle.ID] = triangle
		pair0Set, pair0Exists := pairControl.PairTriangleMap[triangle.Pair0]
		if !pair0Exists {
			pair0Set = make(types.Set)
			pairControl.PairTriangleMap[triangle.Pair0] = pair0Set
		}
		pair1Set, pair1Exists := pairControl.PairTriangleMap[triangle.Pair1]
		if !pair1Exists {
			pair1Set = make(types.Set)
			pairControl.PairTriangleMap[triangle.Pair0] = pair1Set
		}
		pair2Set, pair2Exists := pairControl.PairTriangleMap[triangle.Pair2]
		if !pair2Exists {
			pair2Set = make(types.Set)
			pairControl.PairTriangleMap[triangle.Pair0] = pair2Set
		}
		pair0Set.Add(triangle.ID)
		pair1Set.Add(triangle.ID)
		pair2Set.Add(triangle.ID)
	}

	// 检查是否有遍历中的错误
	if err := rows.Err(); err != nil {
		log.Error("查询失败", "err", err)
	}
}
