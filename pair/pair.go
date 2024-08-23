package pair

import (
	"encoding/json"
	"github.com/ethereum/go-ethereum/common/gopool"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/pair/mysqldb"
	"github.com/ethereum/go-ethereum/pair/pairtypes"
	"github.com/jmoiron/sqlx"
	"os"
	"time"
)

var pairCache pairtypes.PairCache

func init() {
	pairCache = pairtypes.PairCache{}
	pairCache.TriangleMap = make(map[int64]pairtypes.Triangle)
	pairCache.PairTriangleMap = make(map[string]pairtypes.Set)
	fetchTriangleMap()
	err1 := gopool.Submit(timerGetTriangle)
	if err1 != nil {
		log.Error("开启定时加载Triangle任务失败", "err", err1)
		return
	}
	fetchTopicMap()
	err2 := gopool.Submit(timerGetTopic)
	if err2 != nil {
		log.Error("开启定时加载Topic任务失败", "err", err2)
		return
	}
}

func GetPairControl() pairtypes.PairCache {
	return pairCache
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
	pairCache.TopicMap = newTopicMap
}

func fetchTriangleMap() {
	// 初始化数据库连接
	mysqlDB := mysqldb.GetMysqlDB()

	// 使用流式查询，逐行处理数据
	rows, err := mysqlDB.Queryx("SELECT id, token0, router0, pair0, token1, router1, pair1, token2, router2, pair2 FROM arbitrage_triangle LIMIT 0,10")
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
}
