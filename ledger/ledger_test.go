package ledger

import "testing"

func checkAccount(t *testing.T, l *Ledger, addr string, balance, nonce uint64) {
	t.Helper()
	acc := l.Account(addr)
	if acc.Balance != balance || acc.Nonce != nonce {
		t.Errorf("Account(%q) = {Balance:%d Nonce:%d}, want {Balance:%d Nonce:%d}",
			addr, acc.Balance, acc.Nonce, balance, nonce)
	}
}

func checkHead(t *testing.T, l *Ledger, want string) {
	t.Helper()
	if got := l.Head(); got != want {
		t.Errorf("Head() = %q, want %q", got, want)
	}
}

func checkFeePool(t *testing.T, l *Ledger, want uint64) {
	t.Helper()
	if got := l.FeePool(); got != want {
		t.Errorf("FeePool() = %d, want %d", got, want)
	}
}

// 两个分支重复使用同一发送方序号，但形成不同余额分布；
// 随区块到达反复切换链头，每次切换后余额、序号和手续费都必须正确。
func TestForkSwitching(t *testing.T) {
	l := New("G", map[string]uint64{"alice": 100}, 10)

	// 分支 b：b1(alice->carol 50, nonce0)，b2(carol->alice 20, nonce0)
	// 分支 a：a1(alice->bob 30, nonce0)，a2(alice->bob 10 nonce1; bob->carol 5 nonce0)
	b1 := Block{ID: "b1", Parent: "G", Txs: []Tx{{From: "alice", To: "carol", Amount: 50, Nonce: 0}}}
	a1 := Block{ID: "a1", Parent: "G", Txs: []Tx{{From: "alice", To: "bob", Amount: 30, Nonce: 0}}}
	b2 := Block{ID: "b2", Parent: "b1", Txs: []Tx{{From: "carol", To: "alice", Amount: 20, Nonce: 0}}}
	a2 := Block{ID: "a2", Parent: "a1", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 10, Nonce: 1},
		{From: "bob", To: "carol", Amount: 5, Nonce: 0},
	}}

	l.AddBlock(b1) // 高度1，唯一候选
	checkHead(t, l, "b1")
	checkAccount(t, l, "alice", 50, 1)
	checkAccount(t, l, "carol", 50, 0)
	checkAccount(t, l, "bob", 0, 0)
	checkFeePool(t, l, 9)

	l.AddBlock(a1) // 同高，"a1" < "b1"，切换
	checkHead(t, l, "a1")
	checkAccount(t, l, "alice", 70, 1)
	checkAccount(t, l, "bob", 30, 0)
	checkAccount(t, l, "carol", 0, 0)
	checkFeePool(t, l, 9)

	l.AddBlock(b2) // 高度2，切换回 b 分支
	checkHead(t, l, "b2")
	checkAccount(t, l, "alice", 70, 1)
	checkAccount(t, l, "carol", 30, 1)
	checkAccount(t, l, "bob", 0, 0)
	checkFeePool(t, l, 8)

	l.AddBlock(a2) // 高度2，"a2" < "b2"，再次切换
	checkHead(t, l, "a2")
	checkAccount(t, l, "alice", 60, 2)
	checkAccount(t, l, "bob", 35, 1)
	checkAccount(t, l, "carol", 5, 0)
	checkFeePool(t, l, 7)

	// 重复提交不改变任何状态
	l.AddBlock(a1)
	l.AddBlock(b2)
	l.AddBlock(a2)
	checkHead(t, l, "a2")
	checkAccount(t, l, "alice", 60, 2)
	checkAccount(t, l, "bob", 35, 1)
	checkFeePool(t, l, 7)
}

// 子孙先到、祖先后到，并混入重复提交；一条看起来更长的分支在区块中途
// 余额不足，整块及其后代不得成为规范链，也不得污染有效分支。
func TestOutOfOrderAndInvalidBranch(t *testing.T) {
	l := New("G", map[string]uint64{"alice": 100}, 20)

	// 有效分支：v1(alice->bob 10, nonce0)
	v1 := Block{ID: "v1", Parent: "G", Txs: []Tx{{From: "alice", To: "bob", Amount: 10, Nonce: 0}}}
	// 失败分支：x1 合法；x2 第二笔交易时 alice 余额不足，整块无效；x3 是 x2 的后代
	x1 := Block{ID: "x1", Parent: "G", Txs: []Tx{{From: "alice", To: "bob", Amount: 5, Nonce: 0}}}
	x2 := Block{ID: "x2", Parent: "x1", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 90, Nonce: 1}, // 成功：alice 95->5
		{From: "alice", To: "bob", Amount: 10, Nonce: 2}, // 失败：余额不足
	}}
	x3 := Block{ID: "x3", Parent: "x2", Txs: []Tx{{From: "bob", To: "alice", Amount: 1, Nonce: 0}}}
	x4 := Block{ID: "x4", Parent: "x3", Txs: []Tx{{From: "bob", To: "alice", Amount: 1, Nonce: 0}}}

	// 子孙先到，且混入重复提交
	l.AddBlock(x3)
	l.AddBlock(x2)
	l.AddBlock(x3) // 重复（暂存中）
	l.AddBlock(x2) // 重复（暂存中）
	checkHead(t, l, "G")
	checkAccount(t, l, "alice", 100, 0)
	checkFeePool(t, l, 20)

	// 有效分支到达并成为链头
	l.AddBlock(v1)
	checkHead(t, l, "v1")
	checkAccount(t, l, "alice", 90, 1)
	checkAccount(t, l, "bob", 10, 0)
	checkFeePool(t, l, 19)

	// 祖先最后到达：x1 有效但同高字典序更大；x2 整块无效；x3 随之无效
	l.AddBlock(x1)
	checkHead(t, l, "v1")
	checkAccount(t, l, "alice", 90, 1)
	checkAccount(t, l, "bob", 10, 0)
	checkFeePool(t, l, 19)

	// 无效区块的后代同样无效，即使交易本身合法
	l.AddBlock(x4)
	checkHead(t, l, "v1")

	// 重复提交已判定的区块不得重复扣账
	l.AddBlock(x1)
	l.AddBlock(x2)
	l.AddBlock(v1)
	checkHead(t, l, "v1")
	checkAccount(t, l, "alice", 90, 1)
	checkAccount(t, l, "bob", 10, 0)
	checkFeePool(t, l, 19)
}

// 自转账：仍须检查余额、消耗序号和手续费，但最终余额不变。
func TestSelfTransfer(t *testing.T) {
	l := New("G", map[string]uint64{"alice": 10}, 5)
	l.AddBlock(Block{ID: "s1", Parent: "G", Txs: []Tx{
		{From: "alice", To: "alice", Amount: 10, Nonce: 0},
	}})
	checkHead(t, l, "s1")
	checkAccount(t, l, "alice", 10, 1)
	checkFeePool(t, l, 4)

	// 序号必须恰好匹配：nonce 0 已被消耗，重复使用则整块无效
	l.AddBlock(Block{ID: "s2", Parent: "s1", Txs: []Tx{
		{From: "alice", To: "alice", Amount: 1, Nonce: 0},
	}})
	checkHead(t, l, "s1")
	checkAccount(t, l, "alice", 10, 1)
	checkFeePool(t, l, 4)
}

// 手续费池耗尽后，任何含交易的区块都无效。
func TestFeePoolExhaustion(t *testing.T) {
	l := New("G", map[string]uint64{"alice": 10}, 1)
	l.AddBlock(Block{ID: "f1", Parent: "G", Txs: []Tx{
		{From: "alice", To: "bob", Amount: 3, Nonce: 0},
	}})
	checkHead(t, l, "f1")
	checkFeePool(t, l, 0)

	l.AddBlock(Block{ID: "f2", Parent: "f1", Txs: []Tx{
		{From: "bob", To: "alice", Amount: 1, Nonce: 0},
	}})
	checkHead(t, l, "f1")
	checkAccount(t, l, "bob", 3, 0)
}

// 未知账户的余额和序号均为 0。
func TestUnknownAccountIsZero(t *testing.T) {
	l := New("G", nil, 0)
	checkAccount(t, l, "nobody", 0, 0)
	checkHead(t, l, "G")
}
