package forkledger

// Tx 是一笔交易：发送方、接收方、正整数金额与发送方序号。
type Tx struct {
	From   string
	To     string
	Amount uint64
	Nonce  uint64
}

// Block 是一个区块：标识、父块标识与有序交易。
type Block struct {
	ID       string
	ParentID string
	Txs      []Tx
}

// AccountState 是账户在某一链头下的可见状态。
type AccountState struct {
	Balance uint64
	Nonce   uint64
}

// state 是某一区块执行完成后的完整账本状态。
type state struct {
	balances map[string]uint64
	nonces   map[string]uint64
	feePool  uint64
}

func (s *state) clone() *state {
	balances := make(map[string]uint64, len(s.balances))
	for k, v := range s.balances {
		balances[k] = v
	}
	nonces := make(map[string]uint64, len(s.nonces))
	for k, v := range s.nonces {
		nonces[k] = v
	}
	return &state{balances: balances, nonces: nonces, feePool: s.feePool}
}

// apply 尝试执行一笔交易；失败时不改动任何状态并返回 false。
func (s *state) apply(tx Tx) bool {
	if s.balances[tx.From] < tx.Amount {
		return false
	}
	if s.nonces[tx.From] != tx.Nonce {
		return false
	}
	if s.feePool < 1 {
		return false
	}
	s.balances[tx.From] -= tx.Amount
	s.balances[tx.To] += tx.Amount
	s.nonces[tx.From]++
	s.feePool--
	return true
}

// execute 从父状态出发按顺序执行整个区块；任一交易失败则整体失败。
func execute(parent *state, txs []Tx) (*state, bool) {
	next := parent.clone()
	for _, tx := range txs {
		if !next.apply(tx) {
			return nil, false
		}
	}
	return next, true
}

type blockStatus int

const (
	statusPending blockStatus = iota // 父块未知，暂存
	statusValid
	statusInvalid
)

// blockNode 是执行器内部保存的区块节点。
type blockNode struct {
	block    Block
	height   uint64
	status   blockStatus
	post     *state // 仅有效区块拥有执行后状态
	children []*blockNode
}

// Executor 是分叉账本执行器。
type Executor struct {
	genesisID string
	nodes     map[string]*blockNode
	head      *blockNode
	waiting   map[string][]*blockNode // 父块标识 -> 暂存中的子块
}

// NewExecutor 以创世块标识、初始余额与独立手续费池构造执行器。
func NewExecutor(genesisID string, balances map[string]uint64, feePool uint64) *Executor {
	initial := &state{
		balances: make(map[string]uint64, len(balances)),
		nonces:   make(map[string]uint64),
		feePool:  feePool,
	}
	for k, v := range balances {
		initial.balances[k] = v
	}
	genesis := &blockNode{
		block:  Block{ID: genesisID},
		height: 0,
		status: statusValid,
		post:   initial,
	}
	return &Executor{
		genesisID: genesisID,
		nodes:     map[string]*blockNode{genesisID: genesis},
		head:      genesis,
		waiting:   make(map[string][]*blockNode),
	}
}

// AddBlock 接收一个区块；返回前完成所有因此变得可判定的后代。
func (e *Executor) AddBlock(b Block) {
	if _, ok := e.nodes[b.ID]; ok {
		return // 重复提交，直接忽略
	}
	node := &blockNode{block: b, status: statusPending}
	e.nodes[b.ID] = node
	if parent, ok := e.nodes[b.ParentID]; ok {
		parent.children = append(parent.children, node)
	} else {
		e.waiting[b.ParentID] = append(e.waiting[b.ParentID], node)
	}
	for _, child := range e.waiting[b.ID] {
		node.children = append(node.children, child)
	}
	delete(e.waiting, b.ID)
	e.resolve(node)
	e.selectHead()
}

// resolve 判定 node 及其所有因此可判定的后代的状态。
func (e *Executor) resolve(node *blockNode) {
	queue := []*blockNode{node}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n.status != statusPending {
			continue
		}
		parent, ok := e.nodes[n.block.ParentID]
		if !ok || parent.status == statusPending {
			continue // 父块未知或尚未判定，继续暂存
		}
		if parent.status == statusInvalid {
			n.status = statusInvalid // 无效区块的后代均无效
		} else {
			post, ok := execute(parent.post, n.block.Txs)
			if !ok {
				n.status = statusInvalid
			} else {
				n.status = statusValid
				n.height = parent.height + 1
				n.post = post
			}
		}
		queue = append(queue, n.children...)
	}
}

// selectHead 在所有有效区块中选择高度最高、同高标识字典序最小者。
func (e *Executor) selectHead() {
	best := e.head
	for _, n := range e.nodes {
		if n.status != statusValid {
			continue
		}
		if n.height > best.height ||
			(n.height == best.height && n.block.ID < best.block.ID) {
			best = n
		}
	}
	e.head = best
}

// Head 返回当前规范链头的区块标识。
func (e *Executor) Head() string {
	return e.head.block.ID
}

// Account 返回当前规范链头下某账户的状态；不存在时均为 0。
func (e *Executor) Account(address string) AccountState {
	post := e.head.post
	return AccountState{
		Balance: post.balances[address],
		Nonce:   post.nonces[address],
	}
}
