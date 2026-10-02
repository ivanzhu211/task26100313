package ledger

// state 是某个区块执行完成后的账本状态快照。
type state struct {
	accounts map[string]Account
	feePool  uint64
}

func (s *state) clone() *state {
	accounts := make(map[string]Account, len(s.accounts))
	for addr, acc := range s.accounts {
		accounts[addr] = acc
	}
	return &state{accounts: accounts, feePool: s.feePool}
}

// node 是账本中已登记的一个区块。
type node struct {
	block  Block
	height uint64
	valid  bool
	post   *state // 仅 valid 时非空
}

// Ledger 是本地 PoA 链的分叉账本执行器。
type Ledger struct {
	genesisID string
	nodes     map[string]*node   // 所有已登记区块（含创世块）
	pending   map[string][]Block // 父块未知时暂存，键为父块标识
	head      *node
}

// New 构造账本：传入创世块标识、初始账户余额和独立手续费池。
func New(genesisID string, balances map[string]uint64, feePool uint64) *Ledger {
	accounts := make(map[string]Account, len(balances))
	for addr, bal := range balances {
		accounts[addr] = Account{Balance: bal}
	}
	genesis := &node{
		block:  Block{ID: genesisID},
		height: 0,
		valid:  true,
		post:   &state{accounts: accounts, feePool: feePool},
	}
	return &Ledger{
		genesisID: genesisID,
		nodes:     map[string]*node{genesisID: genesis},
		pending:   make(map[string][]Block),
		head:      genesis,
	}
}

// AddBlock 接收一个区块。相同区块可重复提交，父块可能晚于子块到达。
// 返回前会完成所有因此变得可判定的后代区块。
func (l *Ledger) AddBlock(b Block) {
	if _, ok := l.nodes[b.ID]; ok {
		return // 重复提交（含与创世块同标识），直接忽略
	}
	parent, ok := l.nodes[b.Parent]
	if !ok {
		l.pending[b.Parent] = append(l.pending[b.Parent], b)
		return
	}
	l.resolve(b, parent)
}

// resolve 在父块已登记时判定一个区块，并递归处理其暂存的后代。
func (l *Ledger) resolve(b Block, parent *node) {
	n := &node{block: b, height: parent.height + 1}
	if parent.valid {
		if post, ok := execute(parent.post, b.Txs); ok {
			n.valid = true
			n.post = post
		}
	}
	l.nodes[b.ID] = n
	if n.valid && l.better(n, l.head) {
		l.head = n
	}
	children := l.pending[b.ID]
	delete(l.pending, b.ID)
	for _, child := range children {
		if _, ok := l.nodes[child.ID]; ok {
			continue // 暂存期间被重复提交且已处理
		}
		l.resolve(child, n)
	}
}

// better 报告候选区块 a 是否比 b 更适合作为规范链头：
// 高度更高者优先，同高时标识字典序更小者优先。
func (l *Ledger) better(a, b *node) bool {
	if a.height != b.height {
		return a.height > b.height
	}
	return a.ID() < b.ID()
}

// ID 返回节点区块标识。
func (n *node) ID() string { return n.block.ID }

// execute 从父状态起按顺序执行交易。全部成功时返回新状态，
// 任何一笔失败则整个区块无效，返回 ok=false。
func execute(parent *state, txs []Tx) (*state, bool) {
	s := parent.clone()
	for _, tx := range txs {
		from := s.accounts[tx.From]
		if from.Balance < tx.Amount {
			return nil, false
		}
		if tx.Nonce != from.Nonce {
			return nil, false
		}
		if s.feePool < 1 {
			return nil, false
		}
		s.feePool--
		from.Balance -= tx.Amount
		from.Nonce++
		s.accounts[tx.From] = from
		to := s.accounts[tx.To]
		to.Balance += tx.Amount
		s.accounts[tx.To] = to
	}
	return s, true
}

// Head 返回当前规范链头的区块标识。
func (l *Ledger) Head() string {
	return l.head.block.ID
}

// Account 返回当前规范链头下某地址的账户状态。
// 账户不存在时余额和下一个交易序号均为 0。
func (l *Ledger) Account(address string) Account {
	return l.head.post.accounts[address]
}

// FeePool 返回当前规范链头下的手续费池余量。
func (l *Ledger) FeePool() uint64 {
	return l.head.post.feePool
}
