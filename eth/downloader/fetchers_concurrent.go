// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	"errors"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

// timeoutGracePeriod is the amount of time to allow for a peer to deliver a
// response to a locally already timed out request. Timeouts are not penalized
// as a peer might be temporarily overloaded, however, they still must reply
// to each request. Failing to do so is considered a protocol violation.
var timeoutGracePeriod = 2 * time.Minute

// typedQueue is an interface defining the adaptor needed to translate the type
// specific downloader/queue schedulers into the type-agnostic general concurrent
// fetcher algorithm calls.
type typedQueue interface {
	// waker returns a notification channel that gets pinged in case more fetches
	// have been queued up, so the fetcher might assign it to idle peers.
	waker() chan bool

	// pending returns the number of wrapped items that are currently queued for
	// fetching by the concurrent downloader.
	pending() int

	// capacity is responsible for calculating how many items of the abstracted
	// type a particular peer is estimated to be able to retrieve within the
	// allotted round trip time.
	capacity(peer *peerConnection, rtt time.Duration) int

	// updateCapacity is responsible for updating how many items of the abstracted
	// type a particular peer is estimated to be able to retrieve in a unit time.
	updateCapacity(peer *peerConnection, items int, elapsed time.Duration)

	// reserve is responsible for allocating a requested number of pending items
	// from the download queue to the specified peer.
	reserve(peer *peerConnection, items int) (*fetchRequest, bool, bool)

	// unreserve is responsible for removing the current retrieval allocation
	// assigned to a specific peer and placing it back into the pool to allow
	// reassigning to some other peer.
	unreserve(peer string) int

	// request is responsible for converting a generic fetch request into a typed
	// one and sending it to the remote peer for fulfillment.
	request(peer *peerConnection, req *fetchRequest, resCh chan *eth.Response) (*eth.Request, error)

	// deliver is responsible for taking a generic response packet from the
	// concurrent fetcher, unpacking the type specific data and delivering
	// it to the downloader's queue.
	deliver(peer *peerConnection, packet *eth.Response) (int, error)
}

// concurrentFetch iteratively downloads scheduled block parts, taking available
// peers, reserving a chunk of fetch requests for each and waiting for delivery
// or timeouts.
func (d *Downloader) concurrentFetch(queue typedQueue, beaconMode bool) error {
	// Create a delivery channel to accept responses from all peers
	// 创建了一个通道 responses，用于接收来自对等节点的响应数据
	responses := make(chan *eth.Response)

	// Track the currently active requests and their timeout order
	// 创建一个映射 pending，键为对等节点的 id（string 类型），值为请求对象 *eth.Request。用于跟踪当前挂起的请求
	pending := make(map[string]*eth.Request)
	// 使用 defer 延迟执行一个匿名函数，在 concurrentFetch 函数返回时执行。该匿名函数会关闭所有挂起的请求，确保在同步过程取消或出错时资源得到清理
	defer func() {
		// Abort all requests on sync cycle cancellation. The requests may still
		// be fulfilled by the remote side, but the dispatcher will not wait to
		// deliver them since nobody's going to be listening.
		for _, req := range pending {
			req.Close()
		}
	}()
	// 创建一个映射 ordering，用于跟踪每个请求的超时顺序。键为请求对象 *eth.Request，值为它在超时队列中的索引（int 类型）
	ordering := make(map[*eth.Request]int)
	// 创建了一个优先队列 timeouts，用于管理请求的超时事件。队列中的元素按时间排序，时间越早的请求越早被处理。
	// 队列的 New 方法接受一个回调函数，当队列中元素的顺序改变时更新 ordering 映射
	timeouts := prque.New[int64, *eth.Request](func(data *eth.Request, index int) {
		if index < 0 {
			delete(ordering, data)
			return
		}
		ordering[data] = index
	})

	// 创建初始时间为0计时器，停止计时器失败，则计时器触发了，读取它的通道以清理状态
	timeout := time.NewTimer(0)
	if !timeout.Stop() {
		<-timeout.C
	}
	defer timeout.Stop()

	// Track the timed-out but not-yet-answered requests separately. We want to
	// keep tracking which peers are busy (potentially overloaded), so removing
	// all trace of a timed out request is not good. We also can't just cancel
	// the pending request altogether as that would prevent a late response from
	// being delivered, thus never unblocking the peer.
	// 创建一个映射 stales，用于跟踪那些已经超时但尚未收到响应的请求
	stales := make(map[string]*eth.Request)
	defer func() {
		// Abort all requests on sync cycle cancellation. The requests may still
		// be fulfilled by the remote side, but the dispatcher will not wait to
		// deliver them since nobody's going to be listening.
		for _, req := range stales {
			req.Close()
		}
	}()
	// Subscribe to peer lifecycle events to schedule tasks to new joiners and
	// reschedule tasks upon disconnections. We don't care which event happened
	// for simplicity, so just use a single channel.
	// 创建一个缓冲通道 peering，缓冲区大小为 64，用于接收对等节点的加入和离开事件
	peering := make(chan *peeringEvent, 64) // arbitrary buffer, just some burst protection

	peeringSub := d.peers.SubscribeEvents(peering)
	defer peeringSub.Unsubscribe()

	// Prepare the queue and fetch block parts until the block header fetcher's done
	// 初始化一个布尔变量 finished，表示下载过程是否完成
	finished := false
	// 进入一个无限循环，开始处理下载逻辑
	for {
		// Short circuit if we lost all our peers
		// 如果没有任何对等节点且不处于信标模式，返回错误 errNoPeers，表示没有可用的对等节点
		if d.peers.Len() == 0 && !beaconMode {
			return errNoPeers
		}
		// If there's nothing more to fetch, wait or terminate
		// 检查队列中是否还有待处理的任务。如果没有任务且所有请求都已处理完毕，且下载已完成，返回 nil 表示正常结束。否则，继续处理
		if queue.pending() == 0 {
			if len(pending) == 0 && finished {
				return nil
			}
		} else {
			// Send a download request to all idle peers, until throttled
			// 初始化一个切片 idles，用于存储空闲的对等节点，初始化一个切片 caps，用于存储对应对等节点的下载容量
			var (
				idles []*peerConnection
				caps  []int
			)
			// 遍历所有对等节点，检查它们是否空闲或者是否有陈旧请求
			for _, peer := range d.peers.AllPeers() {
				// 如果某个对等节点既没有挂起的请求，也没有陈旧请求，则认为它是空闲的，将其添加到 idles 列表中，并计算它的下载容量 caps
				pending, stale := pending[peer.id], stales[peer.id]
				if pending == nil && stale == nil {
					idles = append(idles, peer)
					caps = append(caps, queue.capacity(peer, time.Second))
				} else if stale != nil {
					// 如果对等节点有陈旧请求，且等待时间超过了允许的宽限期，则认为该节点存在问题，记录日志并丢弃该节点
					if waited := time.Since(stale.Sent); waited > timeoutGracePeriod {
						// Request has been in flight longer than the grace period
						// permitted it, consider the peer malicious attempting to
						// stall the sync.
						peer.log.Warn("Peer stalling, dropping", "waited", common.PrettyDuration(waited))
						d.dropPeer(peer.id)
					}
				}
			}
			// 对空闲对等节点列表进行排序，根据它们的下载容量从大到小排序
			sort.Sort(&peerCapacitySort{idles, caps})

			// 初始化两个布尔变量 progressed 和 throttled，分别表示是否有进展和是否受到限流
			var (
				progressed bool
				throttled  bool
				queued     = queue.pending() // 获取当前队列中待处理任务的数量
			)
			// 遍历空闲的对等节点，为它们分配下载任务
			for _, peer := range idles {
				// Short circuit if throttling activated or there are no more
				// queued tasks to be retrieved
				// 如果限流，则停止为节点分配任务
				if throttled {
					break
				}
				// 如果没有待处理的任务，则退出循环
				if queued = queue.pending(); queued == 0 {
					break
				}
				// Reserve a chunk of fetches for a peer. A nil can mean either that
				// no more headers are available, or that the peer is known not to
				// have them.
				// 尝试为对等节点预留任务，返回请求对象 request、是否有进展 progress、以及是否需要限流 throttle
				request, progress, throttle := queue.reserve(peer, queue.capacity(peer, d.peers.rates.TargetRoundTrip()))
				// 如果有进展，设置 progressed 为 true
				if progress {
					progressed = true
				}
				// 如果需要限流，设置 throttled 为 true 并增加限流计数器
				if throttle {
					throttled = true
					throttleCounter.Inc(1)
				}
				// 如果没有可用的请求，继续下一个循环
				if request == nil {
					continue
				}
				// Fetch the chunk and make sure any errors return the hashes to the queue
				// 执行请求获取块
				req, err := queue.request(peer, request, responses)
				if err != nil {
					// Sending the request failed, which generally means the peer
					// was disconnected in between assignment and network send.
					// Although all peer removal operations return allocated tasks
					// to the queue, that is async, and we can do better here by
					// immediately pushing the unfulfilled requests.
					queue.unreserve(peer.id) // TODO(karalabe): This needs a non-expiration method
					continue
				}
				// 在 pending 中记录该对等节点的当前请求
				pending[peer.id] = req

				ttl := d.peers.rates.TargetTimeout()
				ordering[req] = timeouts.Size()

				timeouts.Push(req, -time.Now().Add(ttl).UnixNano())
				if timeouts.Size() == 1 {
					timeout.Reset(ttl)
				}
			}
			// Make sure that we have peers available for fetching. If all peers have been tried
			// and all failed throw an error
			if !progressed && !throttled && len(pending) == 0 && len(idles) == d.peers.Len() && queued > 0 && !beaconMode {
				return errPeersUnavailable
			}
		}
		// Wait for something to happen
		select {
		// d.cancelCh 通道接收到信号，意味着同步操作被取消
		case <-d.cancelCh:
			// If sync was cancelled, tear down the parallel retriever. Pending
			// requests will be cancelled locally, and the remote responses will
			// be dropped when they arrive
			return errCanceled
		// 如果 peering 通道有事件发生，表示有节点加入或离开
		case event := <-peering:
			// A peer joined or left, the tasks queue and allocations need to be
			// checked for potential assignment or reassignment
			peerid := event.peer.id
			// 如果事件是节点加入
			// 检查该节点是否有未完成的请求。如果有，记录错误日志
			// 检查该节点是否有陈旧的请求。如果有，记录错误日志
			if event.join {
				// Sanity check the internal state; this can be dropped later
				if _, ok := pending[peerid]; ok {
					event.peer.log.Error("Pending request exists for joining peer")
				}
				if _, ok := stales[peerid]; ok {
					event.peer.log.Error("Stale request exists for joining peer")
				}
				// Loop back to the entry point for task assignment
				continue
			}
			// A peer left, any existing requests need to be untracked, pending
			// tasks returned and possible reassignment checked
			// 如果节点离开且有未完成的请求，取消该请求的预留并从 pending 映射中删除该节点
			if req, ok := pending[peerid]; ok {
				queue.unreserve(peerid) // TODO(karalabe): This needs a non-expiration method
				delete(pending, peerid)
				req.Close()
				// 如果请求存在且在 timeouts 队列中，删除超时队列中的对应项
				if index, live := ordering[req]; live {
					if index >= 0 && index < timeouts.Size() {
						timeouts.Remove(index)
						if index == 0 {
							if !timeout.Stop() {
								<-timeout.C
							}
							if timeouts.Size() > 0 {
								_, exp := timeouts.Peek()
								timeout.Reset(time.Until(time.Unix(0, -exp)))
							}
						}
					}
					delete(ordering, req)
				}
			}
			// 如果该节点有陈旧的请求，删除它并关闭请求
			if req, ok := stales[peerid]; ok {
				delete(stales, peerid)
				req.Close()
			}

		case <-timeout.C:
			// Retrieve the next request which should have timed out. The check
			// below is purely for to catch programming errors, given the correct
			// code, there's no possible order of events that should result in a
			// timeout firing for a non-existent event.
			// 如果超时计时器触发，检查下一个应该超时的请求和预计超时时间
			req, exp := timeouts.Peek()
			if now, at := time.Now(), time.Unix(0, -exp); now.Before(at) {
				log.Error("Timeout triggered but not reached", "left", at.Sub(now))
				timeout.Reset(at.Sub(now))
				continue
			}
			// Stop tracking the timed out request from a timing perspective,
			// cancel it, so it's not considered in-flight anymore, but keep
			// the peer marked busy to prevent assigning a second request and
			// overloading it further.
			// 从 pending 中删除超时的请求，将超时的请求加入 stales 中，表示这个请求已经超时
			delete(pending, req.Peer)
			stales[req.Peer] = req

			// 从超时队列中弹出该请求，并重新排序 ordering 中的索引
			timeouts.Pop() // Popping an item will reorder indices in `ordering`, delete after, otherwise will resurrect!
			if timeouts.Size() > 0 {
				_, exp := timeouts.Peek()
				timeout.Reset(time.Until(time.Unix(0, -exp)))
			}
			delete(ordering, req)

			// New timeout potentially set if there are more requests pending,
			// reschedule the failed one to a free peer
			// 释放该请求的任务预留，并尝试将失败的任务重新分配给其他空闲的对等节点
			fails := queue.unreserve(req.Peer)

			// Finally, update the peer's retrieval capacity, or if it's already
			// below the minimum allowance, drop the peer. If a lot of retrieval
			// elements expired, we might have overestimated the remote peer or
			// perhaps ourselves. Only reset to minimal throughput but don't drop
			// just yet.
			//
			// The reason the minimum threshold is 2 is that the downloader tries
			// to estimate the bandwidth and latency of a peer separately, which
			// requires pushing the measured capacity a bit and seeing how response
			// times reacts, to it always requests one more than the minimum (i.e.
			// min 2).
			// 获取发生超时的对等节点，如果该节点已经断开连接，记录错误日志并继续
			peer := d.peers.Peer(req.Peer)
			if peer == nil {
				// If the peer got disconnected in between, we should really have
				// short-circuited it already. Just in case there's some strange
				// codepath, leave this check in not to crash.
				log.Error("Delivery timeout from unknown peer", "peer", req.Peer)
				continue
			}
			// 如果该节点有多次超时，降低其检索能力，否则，直接将该节点从对等节点列表中移除
			if fails > 2 {
				queue.updateCapacity(peer, 0, 0)
			} else {
				d.dropPeer(peer.id)

				// If this peer was the master peer, abort sync immediately
				// 如果该节点是主节点，立即取消同步并返回超时错误
				d.cancelLock.RLock()
				master := peer.id == d.cancelPeer
				d.cancelLock.RUnlock()

				if master {
					d.cancel()
					return errTimeout
				}
			}

		case res := <-responses:
			// Response arrived, it may be for an existing or an already timed
			// out request. If the former, update the timeout heap and perhaps
			// reschedule the timeout timer.
			// 检查响应是否来自一个仍然有效的请求
			index, live := ordering[res.Req]
			// 有效则从超时队列中删除该请求，重置超时计时器，删除该节点的未完成请求，删除该节点的陈旧请求
			if live {
				if index >= 0 && index < timeouts.Size() {
					timeouts.Remove(index)
					if index == 0 {
						if !timeout.Stop() {
							<-timeout.C
						}
						if timeouts.Size() > 0 {
							_, exp := timeouts.Peek()
							timeout.Reset(time.Until(time.Unix(0, -exp)))
						}
					}
				}
				delete(ordering, res.Req)
			}
			// Delete the pending request (if it still exists) and mark the peer idle
			delete(pending, res.Req.Peer)
			delete(stales, res.Req.Peer)

			// Signal the dispatcher that the round trip is done. We'll drop the
			// peer if the data turns out to be junk.
			// 向调度器发送信号，表示该请求的处理已完成
			res.Done <- nil
			res.Req.Close()

			// If the peer was previously banned and failed to deliver its pack
			// in a reasonable time frame, ignore its message.
			// 检查响应是否来自一个有效的对等节点
			if peer := d.peers.Peer(res.Req.Peer); peer != nil {
				// Deliver the received chunk of data and check chain validity
				// 将收到的数据传递给队列并检查其有效性
				accepted, err := queue.deliver(peer, res)
				if errors.Is(err, errInvalidChain) {
					return err
				}
				// Unless a peer delivered something completely else than requested (usually
				// caused by a timed out request which came through in the end), set it to
				// idle. If the delivery's stale, the peer should have already been idled.
				if !errors.Is(err, errStaleDelivery) {
					queue.updateCapacity(peer, accepted, res.Time)
				}
			}

		case cont := <-queue.waker():
			// The header fetcher sent a continuation flag, check if it's done
			if !cont {
				finished = true
			}
		}
	}
}
