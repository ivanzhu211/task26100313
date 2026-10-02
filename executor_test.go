package forkledger

import "testing"

func checkAccount(t *testing.T, e *Executor, addr string, balance, nonce uint64) {
	t.Helper()
	got := e.Account(addr)
	if got.Balance != balance || got.Nonce != nonce {
		t.Fatalf("Account(%s) = {%d, %d}, want {%d, %d}",
			addr, got.Balance, got.Nonce, balance, nonce)
	}
}

func checkHead(t *testing.T, e *Executor, want string) {
	t.Helper()
	if got := e.Head(); got != want {
		t.Fatalf("Head() = %s, want %s", got, want)
	}
}

func checkFeePool(t *testing.T, e *Executor, want uint64) {
	t.Helper()
	if got := e.head.post.feePool; got != want {
		t.Fatalf("feePool = %d, want %d", got, want)
	}
}

// 两个分支重复使用同一发送方序号，形成不同余额分布；
// 反复切换链头后，余额、序号和手续费全部正确。
func TestForkSwitching(t *testing.T) {
	e := NewExecutor("genesis", map[string]uint64{"alice": 100}, 10)

	// 分支一：alice 每笔转 30/20，序号 0/1。
	e.AddBlock(Block{ID: "b1", ParentID: "genesis", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 30, Nonce: 0},
	}})
	e.AddBlock(Block{ID: "b2", ParentID: "b1", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 20, Nonce: 1},
	}})
	checkHead(t, e, "b2")
	checkAccount(t, e, "alice", 50, 2)
	checkAccount(t, e, "bob", 50, 0)
	checkFeePool(t, e, 8)

	// 分支二：同样从序号 0 开始，但金额分布不同，且更长。
	e.AddBlock(Block{ID: "c1", ParentID: "genesis", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 10, Nonce: 0},
	}})
	e.AddBlock(Block{ID: "c2", ParentID: "c1", Txs: []Tx{
		{From: "alice", To: "carol", Amount: 5, Nonce: 1},
	}})
	e.AddBlock(Block{ID: "c3", ParentID: "c2", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 3, Nonce: 2},
	}})
	checkHead(t, e, "c3")
	checkAccount(t, e, "alice", 82, 3)
	checkAccount(t, e, "bob", 13, 0)
	checkAccount(t, e, "carol", 5, 0)
	checkFeePool(t, e, 7)

	// 分支一追到同高：标识字典序更小者胜出（"b3" < "c3"）。
	e.AddBlock(Block{ID: "b3", ParentID: "b2", Txs: []Tx{
		{From: "alice", To: "carol", Amount: 1, Nonce: 2},
	}})
	checkHead(t, e, "b3")
	checkAccount(t, e, "alice", 49, 3)
	checkAccount(t, e, "bob", 50, 0)
	checkAccount(t, e, "carol", 1, 0)
	checkFeePool(t, e, 7)

	// 分支二再次领先，切回分支二的状态。
	e.AddBlock(Block{ID: "c4", ParentID: "c3", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 2, Nonce: 3},
	}})
	checkHead(t, e, "c4")
	checkAccount(t, e, "alice", 80, 4)
	checkAccount(t, e, "bob", 15, 0)
	checkAccount(t, e, "carol", 5, 0)
	checkFeePool(t, e, 6)

	// 分支一反超，再次切换；废弃分支不得留下任何效果。
	e.AddBlock(Block{ID: "b4", ParentID: "b3", Txs: []Tx{
		{From: "alice", To: "alice", Amount: 7, Nonce: 3}, // 自转：余额不变但消耗序号与手续费
	}})
	e.AddBlock(Block{ID: "b5", ParentID: "b4", Txs: []Tx{
		{From: "bob", To: "carol", Amount: 4, Nonce: 0},
	}})
	checkHead(t, e, "b5")
	checkAccount(t, e, "alice", 49, 4)
	checkAccount(t, e, "bob", 46, 1)
	checkAccount(t, e, "carol", 5, 0)
	checkFeePool(t, e, 5)
}

// 子孙先到、祖先后到并混入重复提交；中途余额不足的长分支整块及其后代无效。
func TestOutOfOrderAndInvalidBranch(t *testing.T) {
	e := NewExecutor("genesis", map[string]uint64{"alice": 50}, 10)

	// 有效分支（后到的祖先）：alice 两次各转 10。
	p1 := Block{ID: "p1", ParentID: "genesis", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 10, Nonce: 0},
	}}
	p2 := Block{ID: "p2", ParentID: "p1", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 10, Nonce: 1},
	}}
	// 看似更长的分支：q2 第二笔余额不足，整块无效，q3 随之无效。
	q1 := Block{ID: "q1", ParentID: "genesis", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 40, Nonce: 0},
	}}
	q2 := Block{ID: "q2", ParentID: "q1", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 5, Nonce: 1},
		{From: "alice", To: "bob", Amount: 100, Nonce: 2}, // 余额不足，整块失败
	}}
	q3 := Block{ID: "q3", ParentID: "q2", Txs: []Tx{
		{From: "bob", To: "carol", Amount: 1, Nonce: 0},
	}}

	// 子孙先到，祖先后到，并混入重复提交。
	e.AddBlock(q3)
	e.AddBlock(q2)
	e.AddBlock(p2)
	e.AddBlock(q2)             // 重复提交，不得重复扣账
	e.AddBlock(q3)             // 重复提交
	checkHead(t, e, "genesis") // 全部暂存或无效，链头仍是创世块
	checkAccount(t, e, "alice", 50, 0)
	checkFeePool(t, e, 10)

	e.AddBlock(q1)        // q 链补全：q2 无效 -> q3 无效，链头仍为创世块
	checkHead(t, e, "q1") // q1 有效且高度 1，成为链头；q2/q3 无效
	checkAccount(t, e, "alice", 10, 1)
	checkAccount(t, e, "bob", 40, 0)
	checkFeePool(t, e, 9)

	e.AddBlock(p1) // p2 早已到达，p1 一到应立即级联判定
	checkHead(t, e, "p2")
	checkAccount(t, e, "alice", 30, 2)
	checkAccount(t, e, "bob", 20, 0)
	checkFeePool(t, e, 8)

	// 无效分支不得成为规范链，也不得污染有效分支。
	e.AddBlock(q2)
	e.AddBlock(q3)
	checkHead(t, e, "p2")
	checkAccount(t, e, "alice", 30, 2)
	checkAccount(t, e, "bob", 20, 0)
	checkAccount(t, e, "carol", 0, 0)
	checkFeePool(t, e, 8)
}
